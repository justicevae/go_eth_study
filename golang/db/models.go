package db

import (
	"time"

	"gorm.io/gorm"
)

// 区块链信息
type Chain struct {
	ID         int64  `gorm:"primaryKey"`
	Name       string `gorm:"uniqueIndex"`
	RPCURL     string
	StartBlock uint64
	LastBlock  uint64
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// 合约信息
type Contract struct {
	ID        int64  `gorm:"primaryKey"`
	ChainID   int64  `gorm:"index"`
	Address   string `gorm:"index"`
	Name      string
	Symbol    string
	Decimals  uint8
	CreatedAt time.Time
	UpdatedAt time.Time
}

// 用户余额
type UserBalance struct {
	ID         int64  `gorm:"primaryKey"`
	ChainID    int64  `gorm:"index"`
	ContractID int64  `gorm:"index"`
	UserAddr   string `gorm:"index"`
	Balance    string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// 余额变动记录
type BalanceChange struct {
	ID              int64  `gorm:"primaryKey"`
	ChainID         int64  `gorm:"index"`
	ContractID      int64  `gorm:"index"`
	UserAddr        string `gorm:"index"`
	TransactionHash string `gorm:"index"`
	BlockNumber     uint64
	LogIndex        uint
	FromAddr        string
	ToAddr          string
	Amount          string // 变动金额，正数表示增加，负数表示减少
	EventType       string // "transfer", "mint", "burn"
	BalanceAfter    string // 变动后的余额
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// 用户积分
type UserPoints struct {
	ID          int64  `gorm:"primaryKey"`
	ChainID     int64  `gorm:"index"`
	ContractID  int64  `gorm:"index"`
	UserAddr    string `gorm:"index"`
	TotalPoints string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// 积分计算记录
type PointsCalculation struct {
	ID          int64  `gorm:"primaryKey"`
	ChainID     int64  `gorm:"index"`
	ContractID  int64  `gorm:"index"`
	UserAddr    string `gorm:"index"`
	PeriodStart time.Time
	PeriodEnd   time.Time
	PointsAdded string
	CreatedAt   time.Time
}

func Migrate(db *gorm.DB) error {
	return db.AutoMigrate(
		&Chain{},
		&Contract{},
		&UserBalance{},
		&BalanceChange{},
		&UserPoints{},
		&PointsCalculation{},
	)
}
