package processor

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"gorm.io/gorm"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/justicevae/go_eth_study/config"
	"github.com/justicevae/go_eth_study/contracts"
	"github.com/justicevae/go_eth_study/db"
)

type EventProcessor struct {
	cfg     *config.Config
	db      *gorm.DB
	clients map[int64]*ethclient.Client
	abis    map[int64]*abi.ABI
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	running bool
	mu      sync.Mutex
}

func NewEventProcessor(cfg *config.Config, database *gorm.DB) *EventProcessor {
	ctx, cancel := context.WithCancel(context.Background())

	return &EventProcessor{
		cfg:     cfg,
		db:      database,
		clients: make(map[int64]*ethclient.Client),
		abis:    make(map[int64]*abi.ABI),
		ctx:     ctx,
		cancel:  cancel,
		running: false,
	}
}

// 启动
func (p *EventProcessor) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.running {
		return errors.New("processor already running")
	}

	// 初始化
	for _, chain := range p.cfg.Chains {
		client, err := ethclient.Dial(chain.RPCURL)
		if err != nil {
			return fmt.Errorf("failed to connect to %s: %v", chain.Name, err)
		}
		p.clients[chain.ID] = client

		erc20ABI, err := abi.JSON(strings.NewReader(contracts.ERC20ABI))
		if err != nil {
			return fmt.Errorf("failed to load ERC20 ABI: %v", err)
		}
		p.abis[chain.ID] = &erc20ABI

		var dbChain db.Chain
		result := p.db.First(&dbChain, "id = ?", chain.ID)
		if result.Error != nil {
			if errors.Is(result.Error, gorm.ErrRecordNotFound) {
				p.db.Create(&db.Chain{
					ID:         chain.ID,
					Name:       chain.Name,
					RPCURL:     chain.RPCURL,
					StartBlock: chain.StartBlock,
					LastBlock:  chain.StartBlock - 1,
				})
			} else {
				return fmt.Errorf("failed to check chain %s: %v", chain.Name, result.Error)
			}
		}

		// 初始化合约
		var contract db.Contract
		result = p.db.First(&contract, "chain_id = ? AND address = ?", chain.ID, chain.ContractAddr)
		if result.Error != nil {
			if errors.Is(result.Error, gorm.ErrRecordNotFound) {
				contractAddr := common.HexToAddress(chain.ContractAddr)
				erc20Contract, err := contracts.NewERC20(contractAddr, client)
				if err != nil {
					return fmt.Errorf("failed to create ERC20 contract: %v", err)
				}

				callOpts := &bind.CallOpts{
					Context: p.ctx,
				}

				name, err := erc20Contract.Name(callOpts)
				if err != nil {
					return fmt.Errorf("failed to get contract name: %v", err)
				}

				symbol, err := erc20Contract.Symbol(callOpts)
				if err != nil {
					return fmt.Errorf("failed to get contract symbol: %v", err)
				}

				decimals, err := erc20Contract.Decimals(callOpts)
				if err != nil {
					return fmt.Errorf("failed to get contract decimals: %v", err)
				}

				// 保存合约
				p.db.Create(&db.Contract{
					ChainID:  chain.ID,
					Address:  chain.ContractAddr,
					Name:     name,
					Symbol:   symbol,
					Decimals: decimals,
				})
			} else {
				return fmt.Errorf("failed to check contract %s: %v", chain.ContractAddr, result.Error)
			}
		}
	}

	p.running = true

	for _, chain := range p.cfg.Chains {
		p.wg.Add(1)
		go p.processChain(chain.ID)
	}

	log.Println("Event processor started")
	return nil
}

// 停止事件
func (p *EventProcessor) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.running {
		return
	}

	p.cancel()
	p.wg.Wait()

	for _, client := range p.clients {
		client.Close()
	}

	p.running = false
	log.Println("Event processor stopped")
}

// 处理指定链事件
func (p *EventProcessor) processChain(chainID int64) {
	defer p.wg.Done()

	client, exists := p.clients[chainID]
	if !exists {
		log.Printf("No client found for chain %d", chainID)
		return
	}

	abi, exists := p.abis[chainID]
	if !exists {
		log.Printf("No ABI found for chain %d", chainID)
		return
	}

	var chainConfig config.ChainConfig
	for _, c := range p.cfg.Chains {
		if c.ID == chainID {
			chainConfig = c
			break
		}
	}

	contractAddr := common.HexToAddress(chainConfig.ContractAddr)

	for {
		select {
		case <-p.ctx.Done():
			log.Printf("Stopping event processing for chain %d", chainID)
			return
		default:
			p.processBlocks(chainID, client, abi, contractAddr)
			time.Sleep(time.Duration(p.cfg.Processor.CheckInterval) * time.Second)
		}
	}
}

// 处理区块
func (p *EventProcessor) processBlocks(chainID int64, client *ethclient.Client, abi *abi.ABI, contractAddr common.Address) {
	// 获取最后处理的区块
	var dbChain db.Chain
	result := p.db.First(&dbChain, "id = ?", chainID)
	if result.Error != nil {
		log.Printf("Failed to get chain %d info: %v", chainID, result.Error)
		return
	}

	// 获取当前最新区块
	latestBlock, err := client.BlockNumber(p.ctx)
	if err != nil {
		log.Printf("Failed to get latest block for chain %d: %v", chainID, err)
		return
	}

	// 回滚处理
	safeBlock := latestBlock - p.cfg.Processor.ReorgThreshold
	if safeBlock < dbChain.LastBlock {
		log.Printf("Chain %d reorg detected, rolling back to block %d", chainID, safeBlock)
		p.handleReorg(chainID, safeBlock)
		return
	}

	if safeBlock <= dbChain.LastBlock {
		return
	}

	// 需要处理的区块范围
	startBlock := dbChain.LastBlock + 1
	endBlock := safeBlock

	//
	batchSize := p.cfg.Processor.BlockBatchSize
	for start := startBlock; start <= endBlock; start += batchSize {
		end := start + batchSize - 1
		if end > endBlock {
			end = endBlock
		}

		log.Printf("Processing blocks %d-%d for chain %d", start, end, chainID)

		query := ethereum.FilterQuery{
			FromBlock: big.NewInt(int64(start)),
			ToBlock:   big.NewInt(int64(end)),
			Addresses: []common.Address{contractAddr},
			Topics: [][]common.Hash{
				{
					common.HexToHash("0x123456"),
				},
			},
		}

		logs, err := client.FilterLogs(p.ctx, query)
		if err != nil {
			log.Printf("Failed to filter logs for chain %d, blocks %d-%d: %v", chainID, start, end, err)
			return
		}

		if err := p.processLogs(chainID, contractAddr.Hex(), logs, abi); err != nil {
			log.Printf("Failed to process logs for chain %d: %v", chainID, err)
			return
		}
		dbChain.LastBlock = end
		p.db.Save(&dbChain)
	}

	log.Printf("Processed up to block %d for chain %d", endBlock, chainID)
}

// 处理日志
func (p *EventProcessor) processLogs(chainID int64, contractAddr string, logs []types.Log, abi *abi.ABI) error {
	var contract db.Contract
	result := p.db.First(&contract, "chain_id = ? AND address = ?", chainID, contractAddr)
	if result.Error != nil {
		return fmt.Errorf("failed to get contract: %v", result.Error)
	}

	for _, vLog := range logs {
		if vLog.Topics[0].Hex() == "0x123456" {
			event := struct {
				From  common.Address
				To    common.Address
				Value *big.Int
			}{}

			if err := abi.UnpackIntoInterface(&event, "Transfer", vLog.Data); err != nil {
				log.Printf("Failed to unpack transfer event: %v", err)
				continue
			}

			event.From = common.HexToAddress(vLog.Topics[1].Hex())
			event.To = common.HexToAddress(vLog.Topics[2].Hex())

			// 处理转账事件
			if err := p.handleTransfer(chainID, contract.ID, &vLog, &event); err != nil {
				log.Printf("Failed to handle transfer event: %v", err)
			}
		}
	}

	return nil
}

// 处理转账
func (p *EventProcessor) handleTransfer(chainID int64, contractID int64, log *types.Log, event *struct {
	From  common.Address
	To    common.Address
	Value *big.Int
}) error {
	// 转出
	if event.From != (common.Address{}) {
		fromAddr := event.From.Hex()
		negativeValue := new(big.Int).Neg(event.Value)

		// 更新余额
		if err := p.updateUserBalance(chainID, contractID, fromAddr, negativeValue, log, "transfer"); err != nil {
			return fmt.Errorf("failed to update from address balance: %v", err)
		}
	}

	// 转入
	if event.To != (common.Address{}) {
		toAddr := event.To.Hex()

		// 更新余额
		if err := p.updateUserBalance(chainID, contractID, toAddr, event.Value, log, "transfer"); err != nil {
			return fmt.Errorf("failed to update to address balance: %v", err)
		}
	}

	return nil
}

// 更新用户余额
func (p *EventProcessor) updateUserBalance(chainID int64, contractID int64, userAddr string, amount *big.Int, log *types.Log, eventType string) error {
	return p.db.Transaction(func(tx *gorm.DB) error {
		var balance db.UserBalance
		result := tx.First(&balance, "chain_id = ? AND contract_id = ? AND user_addr = ?",
			chainID, contractID, userAddr)

		var currentBalance *big.Int

		if result.Error != nil {
			if errors.Is(result.Error, gorm.ErrRecordNotFound) {
				currentBalance = big.NewInt(0)
			} else {
				return fmt.Errorf("failed to get user balance: %v", result.Error)
			}
		} else {
			currentBalance, _ = new(big.Int).SetString(balance.Balance, 10)
			if currentBalance == nil {
				return errors.New("invalid balance value")
			}
		}

		// 计算新余额
		newBalance := new(big.Int).Add(currentBalance, amount)

		// 记录余额变动
		balanceChange := db.BalanceChange{
			ChainID:         chainID,
			ContractID:      contractID,
			UserAddr:        userAddr,
			TransactionHash: log.TxHash.Hex(),
			BlockNumber:     log.BlockNumber,
			LogIndex:        log.Index,
			FromAddr:        common.HexToAddress(log.Topics[1].Hex()).Hex(),
			ToAddr:          common.HexToAddress(log.Topics[2].Hex()).Hex(),
			Amount:          amount.String(),
			EventType:       eventType,
			BalanceAfter:    newBalance.String(),
		}

		if err := tx.Create(&balanceChange).Error; err != nil {
			return fmt.Errorf("failed to record balance change: %v", err)
		}

		// 更新用户余额
		if result.Error != nil && errors.Is(result.Error, gorm.ErrRecordNotFound) {
			// 新增用户余额记录
			newBalanceRecord := db.UserBalance{
				ChainID:    chainID,
				ContractID: contractID,
				UserAddr:   userAddr,
				Balance:    newBalance.String(),
			}

			if err := tx.Create(&newBalanceRecord).Error; err != nil {
				return fmt.Errorf("failed to create user balance: %v", err)
			}
		} else {
			// 更新现有余额记录
			balance.Balance = newBalance.String()
			balance.UpdatedAt = time.Now()

			if err := tx.Save(&balance).Error; err != nil {
				return fmt.Errorf("failed to update user balance: %v", err)
			}
		}

		return nil
	})
}

// 处理区块链回滚
func (p *EventProcessor) handleReorg(chainID int64, safeBlock uint64) error {
	return p.db.Transaction(func(tx *gorm.DB) error {
		var changes []db.BalanceChange
		if err := tx.Where("chain_id = ? AND block_number > ?", chainID, safeBlock).
			Order("block_number DESC, log_index DESC").
			Find(&changes).Error; err != nil {
			return fmt.Errorf("failed to find changes to rollback: %v", err)
		}

		if len(changes) == 0 {
			return tx.Model(&db.Chain{}).Where("id = ?", chainID).Update("last_block", safeBlock).Error
		}

		if err := tx.Where("chain_id = ? AND block_number > ?", chainID, safeBlock).
			Delete(&db.BalanceChange{}).Error; err != nil {
			return fmt.Errorf("failed to delete changes: %v", err)
		}

		type userContract struct {
			UserAddr   string
			ContractID int64
		}

		var uniqueUserContracts []userContract
		if err := tx.Model(&db.BalanceChange{}).
			Distinct("user_addr, contract_id").
			Where("chain_id = ? AND block_number <= ?", chainID, safeBlock).
			Find(&uniqueUserContracts).Error; err != nil {
			return fmt.Errorf("failed to find unique user-contract pairs: %v", err)
		}

		for _, uc := range uniqueUserContracts {
			var lastChange db.BalanceChange
			result := tx.Where("chain_id = ? AND contract_id = ? AND user_addr = ? AND block_number <= ?",
				chainID, uc.ContractID, uc.UserAddr, safeBlock).
				Order("block_number DESC, log_index DESC").
				First(&lastChange)

			if result.Error != nil {
				if errors.Is(result.Error, gorm.ErrRecordNotFound) {
					if err := tx.Where("chain_id = ? AND contract_id = ? AND user_addr = ?",
						chainID, uc.ContractID, uc.UserAddr).
						Delete(&db.UserBalance{}).Error; err != nil {
						return fmt.Errorf("failed to delete user balance: %v", err)
					}
				} else {
					return fmt.Errorf("failed to find last change: %v", result.Error)
				}
			} else {
				if err := tx.Model(&db.UserBalance{}).
					Where("chain_id = ? AND contract_id = ? AND user_addr = ?",
						chainID, uc.ContractID, uc.UserAddr).
					Update("balance", lastChange.BalanceAfter).Error; err != nil {
					return fmt.Errorf("failed to update user balance: %v", err)
				}
			}
		}
		return tx.Model(&db.Chain{}).Where("id = ?", chainID).Update("last_block", safeBlock).Error
	})
}
