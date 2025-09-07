package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math/big"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/justicevae/go_eth_study/config"
	"github.com/justicevae/go_eth_study/db"
)

// 积分计算器
type PointCalculator struct {
	cfg     *config.Config
	db      *gorm.DB
	ctx     context.Context
	cancel  context.CancelFunc
	ticker  *time.Ticker
	wg      sync.WaitGroup
	running bool
	mu      sync.Mutex
}

// 创建新的积分计算器
func NewPointCalculator(cfg *config.Config, database *gorm.DB) *PointCalculator {
	ctx, cancel := context.WithCancel(context.Background())

	return &PointCalculator{
		cfg:     cfg,
		db:      database,
		ctx:     ctx,
		cancel:  cancel,
		ticker:  time.NewTicker(time.Duration(cfg.Points.CalculationInterval) * time.Minute),
		running: false,
	}
}

// 启动
func (p *PointCalculator) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.running {
		return errors.New("point calculator already running")
	}

	p.running = true

	p.wg.Add(1)
	go p.calculatePointsLoop()

	log.Println("Point calculator started")
	return nil
}

// 停止
func (p *PointCalculator) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.running {
		return
	}

	p.ticker.Stop()
	p.cancel()
	p.wg.Wait()

	p.running = false
	log.Println("Point calculator stopped")
}

// 积分计算逻辑
func (p *PointCalculator) calculatePointsLoop() {
	defer p.wg.Done()
	p.calculatePoints()

	for {
		select {
		case <-p.ctx.Done():
			log.Println("Stopping point calculation")
			return
		case <-p.ticker.C:
			p.calculatePoints()
		}
	}
}

// 计算积分
func (p *PointCalculator) calculatePoints() {
	log.Println("Starting point calculation")
	now := time.Now()

	// 计算周期的开始时间（当前时间减去计算间隔）
	periodStart := now.Add(-time.Duration(p.cfg.Points.CalculationInterval) * time.Minute)

	var chains []db.Chain
	if err := p.db.Find(&chains).Error; err != nil {
		log.Printf("Failed to get chains: %v", err)
		return
	}

	for _, chain := range chains {
		if err := p.calculateChainPoints(chain.ID, periodStart, now); err != nil {
			log.Printf("Failed to calculate points for chain %d: %v", chain.ID, err)
		}
	}

	log.Printf("Completed point calculation for period %v to %v", periodStart, now)
}

// 计算指定链积分
func (p *PointCalculator) calculateChainPoints(chainID int64, periodStart, periodEnd time.Time) error {
	var contracts []db.Contract
	if err := p.db.Where("chain_id = ?", chainID).Find(&contracts).Error; err != nil {
		return fmt.Errorf("failed to get contracts: %v", err)
	}

	for _, contract := range contracts {
		if err := p.calculateContractPoints(chainID, contract.ID, periodStart, periodEnd); err != nil {
			log.Printf("Failed to calculate points for contract %d on chain %d: %v", contract.ID, chainID, err)
		}
	}

	return nil
}

// 计算指定合约积分
func (p *PointCalculator) calculateContractPoints(chainID, contractID int64, periodStart, periodEnd time.Time) error {
	// 获取该合约在计算周期内有余额变动的所有用户
	type userBalanceChange struct {
		UserAddr string
	}

	var users []userBalanceChange
	if err := p.db.Model(&db.BalanceChange{}).
		Distinct("user_addr").
		Where("chain_id = ? AND contract_id = ? AND created_at BETWEEN ? AND ?",
			chainID, contractID, periodStart, periodEnd).
		Find(&users).Error; err != nil {
		return fmt.Errorf("failed to get users with balance changes: %v", err)
	}

	// 获取当前有余额但在周期内没有变动的用户
	var currentUsers []userBalanceChange
	if err := p.db.Model(&db.UserBalance{}).Distinct("user_addr").Where("chain_id = ? AND contract_id = ?", chainID, contractID).
		Not("user_addr IN (?)", p.db.Model(&db.BalanceChange{}).
			Select("user_addr").
			Where("chain_id = ? AND contract_id = ? AND created_at BETWEEN ? AND ?", chainID, contractID, periodStart, periodEnd)).
		Find(&currentUsers).Error; err != nil {
		return fmt.Errorf("failed to get users with no balance changes: %v", err)
	}
	allUsers := append(users, currentUsers...)
	for _, user := range allUsers {
		if err := p.calculateUserPoints(chainID, contractID, user.UserAddr, periodStart, periodEnd); err != nil {
			log.Printf("Failed to calculate points for user %s on contract %d, chain %d: %v",
				user.UserAddr, contractID, chainID, err)
		}
	}

	return nil
}

// 计算指定用户的积分
func (p *PointCalculator) calculateUserPoints(chainID, contractID int64, userAddr string, periodStart, periodEnd time.Time) error {
	return p.db.Transaction(func(tx *gorm.DB) error {
		var startBalanceStr string

		// 查找周期开始前的最后一次余额变动
		var lastChangeBefore db.BalanceChange
		result := tx.Where("chain_id = ? AND contract_id = ? AND user_addr = ? AND created_at <= ?",
			chainID, contractID, userAddr, periodStart).
			Order("created_at DESC").
			First(&lastChangeBefore)

		if result.Error != nil {
			if errors.Is(result.Error, gorm.ErrRecordNotFound) {
				startBalanceStr = "0"
			} else {
				return fmt.Errorf("failed to find last balance change before period: %v", result.Error)
			}
		} else {
			startBalanceStr = lastChangeBefore.BalanceAfter
		}

		// 获取该用户在计算周期内的所有余额变动
		var changes []db.BalanceChange
		if err := tx.Where("chain_id = ? AND contract_id = ? AND user_addr = ? AND created_at BETWEEN ? AND ?",
			chainID, contractID, userAddr, periodStart, periodEnd).
			Order("created_at ASC").
			Find(&changes).Error; err != nil {
			return fmt.Errorf("failed to get balance changes in period: %v", err)
		}

		// 计算该周期内的积分
		points, err := p.calculatePeriodPoints(startBalanceStr, changes, periodStart, periodEnd)
		if err != nil {
			return fmt.Errorf("failed to calculate period points: %v", err)
		}

		if points.Cmp(big.NewInt(0)) <= 0 {
			return nil
		}

		// 更新用户总积分
		var userPoints db.UserPoints
		result = tx.First(&userPoints, "chain_id = ? AND contract_id = ? AND user_addr = ?",
			chainID, contractID, userAddr)

		var totalPoints *big.Int

		if result.Error != nil {
			if errors.Is(result.Error, gorm.ErrRecordNotFound) {
				totalPoints = new(big.Int).Set(points)

				// 创建新的积分记录
				userPoints = db.UserPoints{
					ChainID:     chainID,
					ContractID:  contractID,
					UserAddr:    userAddr,
					TotalPoints: totalPoints.String(),
				}

				if err := tx.Create(&userPoints).Error; err != nil {
					return fmt.Errorf("failed to create user points: %v", err)
				}
			} else {
				return fmt.Errorf("failed to get user points: %v", result.Error)
			}
		} else {
			existingPoints, ok := new(big.Int).SetString(userPoints.TotalPoints, 10)
			if !ok {
				return errors.New("invalid total points value")
			}
			totalPoints = new(big.Int).Add(existingPoints, points)

			// 更新总积分
			userPoints.TotalPoints = totalPoints.String()
			userPoints.UpdatedAt = time.Now()

			if err := tx.Save(&userPoints).Error; err != nil {
				return fmt.Errorf("failed to update user points: %v", err)
			}
		}

		// 记录本次积分计算
		calculation := db.PointsCalculation{
			ChainID:     chainID,
			ContractID:  contractID,
			UserAddr:    userAddr,
			PeriodStart: periodStart,
			PeriodEnd:   periodEnd,
			PointsAdded: points.String(),
		}

		if err := tx.Create(&calculation).Error; err != nil {
			return fmt.Errorf("failed to record points calculation: %v", err)
		}

		log.Printf("Calculated %s points for user %s on contract %d, chain %d",
			points.String(), userAddr, contractID, chainID)

		return nil
	})
}

// 计算周期内的积分
func (p *PointCalculator) calculatePeriodPoints(startBalanceStr string, changes []db.BalanceChange, periodStart, periodEnd time.Time) (*big.Int, error) {
	startBalance, ok := new(big.Int).SetString(startBalanceStr, 10)
	if !ok {
		return nil, errors.New("invalid start balance value")
	}

	totalPoints := big.NewInt(0)
	currentBalance := new(big.Int).Set(startBalance)
	segmentStartTime := periodStart

	for _, change := range changes {
		changeTime := change.CreatedAt

		duration := changeTime.Sub(segmentStartTime)
		if duration > 0 {
			segmentPoints := calculateSegmentPoints(currentBalance, duration, p.cfg.Points.Rate)
			totalPoints.Add(totalPoints, segmentPoints)
		}

		newBalance, ok := new(big.Int).SetString(change.BalanceAfter, 10)
		if !ok {
			return nil, errors.New("invalid balance after value")
		}
		currentBalance.Set(newBalance)
		segmentStartTime = changeTime
	}
	lastDuration := periodEnd.Sub(segmentStartTime)
	if lastDuration > 0 {
		segmentPoints := calculateSegmentPoints(currentBalance, lastDuration, p.cfg.Points.Rate)
		totalPoints.Add(totalPoints, segmentPoints)
	}

	return totalPoints, nil
}

// 计算时间段内的积分
func calculateSegmentPoints(balance *big.Int, duration time.Duration, rate float64) *big.Int {
	minutes := duration.Minutes()

	rateNumerator := big.NewInt(5)     // 0.05的分子
	rateDenominator := big.NewInt(100) // 0.05的分母

	// 将分钟转换为整数
	minutesInt := big.NewInt(int64(minutes * 1000000))
	minutesDenominator := big.NewInt(1000000)

	// 计算: balance * rateNumerator * minutesInt / (rateDenominator * 60 * minutesDenominator)
	numerator := new(big.Int).Mul(balance, rateNumerator)
	numerator.Mul(numerator, minutesInt)

	denominator := new(big.Int).Mul(rateDenominator, big.NewInt(60))
	denominator.Mul(denominator, minutesDenominator)

	result := new(big.Int).Div(numerator, denominator)
	return result
}
