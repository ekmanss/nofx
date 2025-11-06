// Package market 提供加密货币市场监控功能
package market

import (
	"encoding/json" // JSON 编解码
	"fmt"           // 格式化输入输出
	"log"           // 日志记录
	"strings"       // 字符串处理
	"sync"          // 并发同步原语（如 sync.Map, sync.WaitGroup）
	"time"          // 时间处理
)

// WSMonitor WebSocket 监控器结构体
// 负责管理多个交易对的实时数据监控
type WSMonitor struct {
	wsClient       *WSClient              // WebSocket 客户端，用于单个流连接
	combinedClient *CombinedStreamsClient // 组合流客户端，用于批量订阅多个交易对
	symbols        []string               // 需要监控的交易对列表，如 ["BTCUSDT", "ETHUSDT"]
	featuresMap    sync.Map               // 特征数据映射（线程安全的 map）
	alertsChan     chan Alert             // 告警通道，容量为 1000，用于发送交易告警
	klineDataMap3m sync.Map               // 存储每个交易对的 3 分钟 K 线历史数据（线程安全）
	klineDataMap4h sync.Map               // 存储每个交易对的 4 小时 K 线历史数据（线程安全）
	tickerDataMap  sync.Map               // 存储每个交易对的 ticker（行情）数据（线程安全）
	batchSize      int                    // 批量订阅的批次大小
	filterSymbols  sync.Map               // 过滤后需要监控的币种及其状态（线程安全）
	symbolStats    sync.Map               // 存储币种统计信息（线程安全）
	FilterSymbol   []string               // 经过筛选的币种列表（导出字段，首字母大写）
}

// SymbolStats 币种统计信息结构体
// 用于跟踪每个交易对的活动状态和评分
type SymbolStats struct {
	LastActiveTime   time.Time // 最后活跃时间
	AlertCount       int       // 告警次数
	VolumeSpikeCount int       // 成交量激增次数
	LastAlertTime    time.Time // 最后告警时间
	Score            float64   // 综合评分（用于排序或筛选）
}

// WSMonitorCli 全局 WebSocket 监控器实例
// 在 Go 中，包级别的变量可以被其他包访问（首字母大写）
var WSMonitorCli *WSMonitor

// subKlineTime 订阅的 K 线时间周期
// 这里配置了 3 分钟和 4 小时两个周期
var subKlineTime = []string{"3m", "4h"}

// NewWSMonitor 创建新的 WebSocket 监控器
// 参数:
//   - batchSize: 批量订阅的批次大小，控制单次订阅的交易对数量
//
// 返回值:
//   - *WSMonitor: 新创建的监控器实例指针
//
// Go 知识点:
//   - 这是一个构造函数模式，Go 没有构造函数，通常使用 NewXxx 函数
//   - make(chan Alert, 1000) 创建一个带缓冲区的通道，容量为 1000
//   - &WSMonitor{...} 创建结构体并返回其指针
func NewWSMonitor(batchSize int) *WSMonitor {
	WSMonitorCli = &WSMonitor{
		wsClient:       NewWSClient(),
		combinedClient: NewCombinedStreamsClient(batchSize),
		alertsChan:     make(chan Alert, 1000),
		batchSize:      batchSize,
	}
	return WSMonitorCli
}

// Initialize 初始化监控器
// 参数:
//   - coins: 需要监控的交易对列表，如果为空则获取所有 USDT 永续合约
//
// 返回值:
//   - error: 错误信息，成功返回 nil
//
// Go 知识点:
//   - (m *WSMonitor) 是方法接收者，表示这是 WSMonitor 的方法
//   - []string 是字符串切片（动态数组）
//   - error 是 Go 的内置错误类型
func (m *WSMonitor) Initialize(coins []string) error {
	log.Println("初始化WebSocket监控器...")
	// 获取交易对信息
	apiClient := NewAPIClient()

	// 如果不指定交易对，则使用 market 市场的所有交易对币种
	if len(coins) == 0 {
		// 从交易所 API 获取交易对信息
		exchangeInfo, err := apiClient.GetExchangeInfo()
		if err != nil {
			return err // 如果出错，直接返回错误
		}

		// 筛选永续合约交易对 --仅测试时使用
		//exchangeInfo.Symbols = exchangeInfo.Symbols[0:2]

		// 遍历所有交易对，筛选符合条件的
		for _, symbol := range exchangeInfo.Symbols {
			// 条件：1. 状态为交易中 2. 是永续合约 3. 以 USDT 结尾
			// symbol.Symbol[len(symbol.Symbol)-4:] 获取最后 4 个字符
			if symbol.Status == "TRADING" &&
				symbol.ContractType == "PERPETUAL" &&
				strings.ToUpper(symbol.Symbol[len(symbol.Symbol)-4:]) == "USDT" {
				m.symbols = append(m.symbols, symbol.Symbol) // 添加到监控列表
				m.filterSymbols.Store(symbol.Symbol, true)   // 存储到过滤 Map
			}
		}
	} else {
		// 如果指定了交易对，直接使用
		m.symbols = coins
	}

	log.Printf("找到 %d 个交易对", len(m.symbols))

	// 初始化历史数据
	if err := m.initializeHistoricalData(); err != nil {
		log.Printf("初始化历史数据失败: %v", err)
	}

	return nil
}

// initializeHistoricalData 初始化历史K线数据
// 这个函数使用并发方式获取所有交易对的历史数据，提高初始化速度
//
// Go 知识点：
//   - sync.WaitGroup: 用于等待一组 goroutine 完成
//   - semaphore (信号量): 使用通道实现，限制并发数为 5
//   - goroutine: 使用 go 关键字启动的轻量级线程
//   - defer: 延迟执行，函数返回前执行
func (m *WSMonitor) initializeHistoricalData() error {
	apiClient := NewAPIClient()

	var wg sync.WaitGroup               // WaitGroup 用于等待所有 goroutine 完成
	semaphore := make(chan struct{}, 5) // 创建容量为 5 的通道，用作信号量限制并发

	// 遍历所有交易对
	for _, symbol := range m.symbols {
		wg.Add(1)               // WaitGroup 计数器加 1
		semaphore <- struct{}{} // 向信号量发送数据，如果已满会阻塞（限制并发）

		// 启动 goroutine 并发获取数据
		// 注意：将 symbol 作为参数传入，避免闭包问题
		go func(s string) {
			defer wg.Done()                // 函数结束时 WaitGroup 计数器减 1
			defer func() { <-semaphore }() // 函数结束时从信号量接收数据，释放一个槽位

			// 获取 3 分钟 K 线历史数据（最近 100 条）
			klines, err := apiClient.GetKlines(s, "3m", 100)
			if err != nil {
				log.Printf("获取 %s 历史数据失败: %v", s, err)
				return
			}
			if len(klines) > 0 {
				m.klineDataMap3m.Store(s, klines) // 存储到线程安全的 Map
				log.Printf("已加载 %s 的历史K线数据-3m: %d 条", s, len(klines))
			}

			// 获取 4 小时 K 线历史数据（最近 100 条）
			klines4h, err := apiClient.GetKlines(s, "4h", 100)
			if err != nil {
				log.Printf("获取 %s 历史数据失败: %v", s, err)
				return
			}
			if len(klines4h) > 0 {
				m.klineDataMap4h.Store(s, klines4h) // 存储到线程安全的 Map
				log.Printf("已加载 %s 的历史K线数据-4h: %d 条", s, len(klines4h))
			}
		}(symbol) // 将 symbol 作为参数传入 goroutine
	}

	wg.Wait() // 阻塞等待所有 goroutine 完成
	return nil
}

// Start 启动 WebSocket 监控器
// 这是监控器的主入口函数，负责初始化、连接和订阅
//
// 参数:
//   - coins: 需要监控的交易对列表
//
// Go 知识点:
//   - log.Fatalf: 打印错误并退出程序（使用 os.Exit(1)）
func (m *WSMonitor) Start(coins []string) {
	log.Printf("启动WebSocket实时监控...")

	// 步骤 1: 初始化交易对列表和历史数据
	err := m.Initialize(coins)
	if err != nil {
		log.Fatalf("❌ 初始化币种: %v", err)
		return
	}

	// 步骤 2: 建立 WebSocket 连接
	err = m.combinedClient.Connect()
	if err != nil {
		log.Fatalf("❌ 批量订阅流: %v", err)
		return
	}

	// 步骤 3: 订阅所有交易对的数据流
	err = m.subscribeAll()
	if err != nil {
		log.Fatalf("❌ 订阅币种交易对: %v", err)
		return
	}
}

// subscribeSymbol 为单个交易对订阅指定时间周期的 K 线数据
// 参数:
//   - symbol: 交易对符号（如 "BTCUSDT"）
//   - st: 时间周期（如 "3m", "4h"）
//
// 返回值:
//   - []string: 订阅的流名称列表
//
// Go 知识点:
//   - fmt.Sprintf: 格式化字符串（类似 C 语言的 sprintf）
//   - strings.ToLower: 将字符串转为小写
//   - go 关键字: 启动新的 goroutine 异步处理数据
func (m *WSMonitor) subscribeSymbol(symbol, st string) []string {
	var streams []string
	// 构造流名称，格式: "btcusdt@kline_3m"
	stream := fmt.Sprintf("%s@kline_%s", strings.ToLower(symbol), st)
	// 添加订阅者，获取数据通道（容量 100）
	ch := m.combinedClient.AddSubscriber(stream, 100)
	streams = append(streams, stream)
	// 启动 goroutine 处理 K 线数据
	go m.handleKlineData(symbol, ch, st)

	return streams
}

// subscribeAll 批量订阅所有交易对的数据流
// 为每个交易对订阅多个时间周期的 K 线数据
//
// 返回值:
//   - error: 订阅失败时返回错误
func (m *WSMonitor) subscribeAll() error {
	log.Println("开始订阅所有交易对...")

	// 第一轮：为每个交易对创建订阅者和数据处理 goroutine
	for _, symbol := range m.symbols {
		for _, st := range subKlineTime { // 遍历时间周期 ["3m", "4h"]
			m.subscribeSymbol(symbol, st)
		}
	}

	// 第二轮：执行批量订阅请求（向服务器发送订阅命令）
	for _, st := range subKlineTime {
		err := m.combinedClient.BatchSubscribeKlines(m.symbols, st)
		if err != nil {
			log.Fatalf("❌ 订阅3m K线: %v", err)
			return err
		}
	}

	log.Println("所有交易对订阅完成")
	return nil
}

// handleKlineData 处理 K 线数据流
// 这个函数在 goroutine 中运行，持续从通道接收并处理数据
//
// 参数:
//   - symbol: 交易对符号
//   - ch: 只读通道（<-chan），接收原始 JSON 数据
//   - _time: 时间周期
//
// Go 知识点:
//   - for ... range ch: 循环接收通道数据，通道关闭时自动退出
//   - <-chan []byte: 只读通道类型，只能接收数据
//   - json.Unmarshal: 将 JSON 字节数组解析为 Go 结构体
//   - continue: 跳过当前循环，继续下一次
func (m *WSMonitor) handleKlineData(symbol string, ch <-chan []byte, _time string) {
	// 持续从通道接收数据，直到通道关闭
	for data := range ch {
		var klineData KlineWSData
		// 将 JSON 数据解析为 KlineWSData 结构体
		if err := json.Unmarshal(data, &klineData); err != nil {
			log.Printf("解析Kline数据失败: %v", err)
			continue // 跳过错误数据，继续处理下一条
		}
		// 处理 K 线更新
		m.processKlineUpdate(symbol, klineData, _time)
	}
}

// getKlineDataMap 根据时间周期获取对应的 K 线数据存储 Map
// 参数:
//   - _time: 时间周期（"3m" 或 "4h"）
//
// 返回值:
//   - *sync.Map: 对应时间周期的数据存储 Map 指针
//
// Go 知识点:
//   - 返回指针而不是值，避免复制整个 sync.Map
//   - sync.Map 是线程安全的 Map，适用于并发场景
func (m *WSMonitor) getKlineDataMap(_time string) *sync.Map {
	var klineDataMap *sync.Map
	if _time == "3m" {
		klineDataMap = &m.klineDataMap3m
	} else if _time == "4h" {
		klineDataMap = &m.klineDataMap4h
	} else {
		// 如果是其他时间周期，返回新的空 Map
		klineDataMap = &sync.Map{}
	}
	return klineDataMap
}

// processKlineUpdate 处理 K 线数据更新
// 将 WebSocket 接收的 K 线数据转换并存储到内存中
//
// 参数:
//   - symbol: 交易对符号
//   - wsData: WebSocket 接收的原始 K 线数据
//   - _time: 时间周期
//
// Go 知识点:
//   - parseFloat: 将字符串转换为 float64（忽略错误用 _）
//   - value.([]Kline): 类型断言，将 interface{} 转换为 []Kline
//   - klines[len(klines)-1]: 访问切片最后一个元素
//   - klines[1:]: 切片操作，从索引 1 到末尾（移除第一个元素）
func (m *WSMonitor) processKlineUpdate(symbol string, wsData KlineWSData, _time string) {
	// 步骤 1: 转换 WebSocket 数据为 Kline 结构
	kline := Kline{
		OpenTime:  wsData.Kline.StartTime,
		CloseTime: wsData.Kline.CloseTime,
		Trades:    wsData.Kline.NumberOfTrades,
	}
	// 解析字符串价格为 float64 数值
	// _ 表示忽略错误返回值（在生产环境建议处理错误）
	kline.Open, _ = parseFloat(wsData.Kline.OpenPrice)
	kline.High, _ = parseFloat(wsData.Kline.HighPrice)
	kline.Low, _ = parseFloat(wsData.Kline.LowPrice)
	kline.Close, _ = parseFloat(wsData.Kline.ClosePrice)
	kline.Volume, _ = parseFloat(wsData.Kline.Volume)
	kline.High, _ = parseFloat(wsData.Kline.HighPrice) // 重复赋值，可能是笔误
	kline.QuoteVolume, _ = parseFloat(wsData.Kline.QuoteVolume)
	kline.TakerBuyBaseVolume, _ = parseFloat(wsData.Kline.TakerBuyBaseVolume)
	kline.TakerBuyQuoteVolume, _ = parseFloat(wsData.Kline.TakerBuyQuoteVolume)

	// 步骤 2: 更新 K 线数据到缓存
	var klineDataMap = m.getKlineDataMap(_time)
	value, exists := klineDataMap.Load(symbol) // 从 Map 中加载数据
	var klines []Kline

	if exists {
		// 如果数据已存在，进行类型断言
		klines = value.([]Kline)

		// 检查是否是新的 K 线（通过 OpenTime 判断）
		if len(klines) > 0 && klines[len(klines)-1].OpenTime == kline.OpenTime {
			// 相同时间：更新当前 K 线（K 线未关闭，实时更新）
			klines[len(klines)-1] = kline
		} else {
			// 不同时间：添加新 K 线（新的 K 线周期开始）
			klines = append(klines, kline)

			// 保持数据长度不超过 100 条（滑动窗口）
			if len(klines) > 100 {
				klines = klines[1:] // 移除最旧的一条数据
			}
		}
	} else {
		// 如果数据不存在，创建新的切片
		klines = []Kline{kline}
	}

	// 步骤 3: 将更新后的数据存回 Map
	klineDataMap.Store(symbol, klines)
}

// GetCurrentKlines 获取指定交易对的当前 K 线数据
// 如果数据不存在，会动态订阅并获取数据（懒加载模式）
//
// 参数:
//   - symbol: 交易对符号
//   - _time: 时间周期
//
// 返回值:
//   - []Kline: K 线数据切片
//   - error: 错误信息
//
// Go 知识点:
//   - 多返回值：Go 函数可以返回多个值
//   - fmt.Errorf: 创建格式化的错误信息
//   - 懒加载：数据不存在时才加载，提高启动速度
func (m *WSMonitor) GetCurrentKlines(symbol string, _time string) ([]Kline, error) {
	// 尝试从缓存中加载数据
	value, exists := m.getKlineDataMap(_time).Load(symbol)

	if !exists {
		// 数据不存在：动态获取并订阅
		// 这是一个兼容性设计，防止在初始化未完成时就有请求进来
		log.Printf("缓存中不存在 %s 的数据，开始动态获取", symbol)

		// 通过 API 获取历史数据
		apiClient := NewAPIClient()
		klines, err := apiClient.GetKlines(symbol, _time, 100)

		// 将数据缓存到内存中
		m.getKlineDataMap(_time).Store(strings.ToUpper(symbol), klines)

		// 动态订阅该交易对，以便后续实时更新
		subStr := m.subscribeSymbol(symbol, _time)
		subErr := m.combinedClient.subscribeStreams(subStr)
		log.Printf("动态订阅流: %v", subStr)

		// 错误处理
		if subErr != nil {
			return nil, fmt.Errorf("动态订阅%v分钟K线失败: %v", _time, subErr)
		}
		if err != nil {
			return nil, fmt.Errorf("获取%v分钟K线失败: %v", _time, err)
		}

		// 注意：这里返回了数据但同时返回错误，可能需要优化
		return klines, fmt.Errorf("symbol不存在")
	}

	// 数据存在：直接返回（需要类型断言）
	return value.([]Kline), nil
}

// Close 关闭监控器，释放资源
//
// Go 知识点:
//   - close(channel): 关闭通道，通知所有接收者不再有数据
//   - 关闭通道后，range 循环会自动退出
//   - 资源清理：关闭连接、释放通道等
func (m *WSMonitor) Close() {
	m.wsClient.Close()  // 关闭 WebSocket 连接
	close(m.alertsChan) // 关闭告警通道
}
