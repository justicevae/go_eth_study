package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/justicevae/go_eth_study/config"
	"github.com/justicevae/go_eth_study/db"
	"github.com/justicevae/go_eth_study/processor"
	"github.com/justicevae/go_eth_study/service"
)

func main() {
	// 解析命令行参数
	configPath := flag.String("config", "config.yaml", "Path to configuration file")
	flag.Parse()

	// 加载配置
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// 初始化数据库连接
	database, err := db.InitDB(cfg.Database)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.CloseDB(database)

	// 初始化事件处理器
	eventProcessor := processor.NewEventProcessor(cfg, database)

	// 启动事件处理
	go eventProcessor.Start()

	// 初始化积分计算器
	pointCalculator := service.NewPointCalculator(cfg, database)

	// 启动定时任务计算积分
	go pointCalculator.Start()

	// 等待中断信号优雅退出
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	// 停止服务
	eventProcessor.Stop()
	pointCalculator.Stop()

	log.Println("Service stopped gracefully")
}
