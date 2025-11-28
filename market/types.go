package market

import "time"

// Data 市场数据结构
type Data struct {
	Symbol       string
	CurrentPrice float64
	Weekly       *WeeklyData
	Daily        *DailyData
	FourHour     *FourHourData
	OneHour      *OneHourData
	FundingRates []FundingRate
}

// WeeklyData 周线数据
type WeeklyData struct {
	Klines     []Kline
	Indicators WeeklyIndicators
}

// WeeklyIndicators 周线指标
type WeeklyIndicators struct {
	SMA50  []float64
	SMA200 []float64
	EMA20  []float64
}

// DailyData 日线数据（仅保留最近 250 根）
type DailyData struct {
	Klines     []Kline
	Indicators DailyIndicators
}

// FourHourData 4小时数据（仅保留最近 200 根）
type FourHourData struct {
	Klines     []Kline
	Indicators FourHourIndicators
}

// OneHourData 1小时数据（仅保留最近 200 根）
type OneHourData struct {
	Klines     []Kline
	Indicators OneHourIndicators
}

// DailyIndicators 日线指标
type DailyIndicators struct {
	SMA50      []float64
	SMA200     []float64
	EMA20      []float64
	MACDLine   []float64
	MACDSignal []float64
	MACDHist   []float64
	RSI14      []float64
	ATR14      []float64
}

// FourHourIndicators 4小时指标
type FourHourIndicators struct {
	EMA20          []float64
	EMA50          []float64
	EMA100         []float64
	EMA200         []float64
	MACDLine       []float64
	MACDSignal     []float64
	MACDHist       []float64
	RSI14          []float64
	ATR14          []float64
	ADX14          []float64
	PlusDI14       []float64
	MinusDI14      []float64
	BollUpper20_2  []float64
	BollMiddle20_2 []float64
	BollLower20_2  []float64
}

// OneHourIndicators 1小时指标
type OneHourIndicators struct {
	EMA20          []float64
	EMA50          []float64
	RSI7           []float64
	RSI14          []float64
	BollUpper20_2  []float64
	BollMiddle20_2 []float64
	BollLower20_2  []float64
}

// Binance API 响应结构
type ExchangeInfo struct {
	Symbols []SymbolInfo `json:"symbols"`
}

type SymbolInfo struct {
	Symbol            string `json:"symbol"`
	Status            string `json:"status"`
	BaseAsset         string `json:"baseAsset"`
	QuoteAsset        string `json:"quoteAsset"`
	ContractType      string `json:"contractType"`
	PricePrecision    int    `json:"pricePrecision"`
	QuantityPrecision int    `json:"quantityPrecision"`
}

type Kline struct {
	OpenTime            int64   `json:"openTime"`
	Open                float64 `json:"open"`
	High                float64 `json:"high"`
	Low                 float64 `json:"low"`
	Close               float64 `json:"close"`
	Volume              float64 `json:"volume"`
	CloseTime           int64   `json:"closeTime"`
	QuoteVolume         float64 `json:"quoteVolume"`
	Trades              int     `json:"trades"`
	TakerBuyBaseVolume  float64 `json:"takerBuyBaseVolume"`
	TakerBuyQuoteVolume float64 `json:"takerBuyQuoteVolume"`
}

type KlineResponse []interface{}

type PriceTicker struct {
	Symbol string `json:"symbol"`
	Price  string `json:"price"`
}

// FundingRate 资金费率历史条目
type FundingRate struct {
	Symbol      string
	FundingRate float64
	FundingTime int64
	MarkPrice   float64
}

type Ticker24hr struct {
	Symbol             string `json:"symbol"`
	PriceChange        string `json:"priceChange"`
	PriceChangePercent string `json:"priceChangePercent"`
	Volume             string `json:"volume"`
	QuoteVolume        string `json:"quoteVolume"`
}

// 特征数据结构
type SymbolFeatures struct {
	Symbol           string    `json:"symbol"`
	Timestamp        time.Time `json:"timestamp"`
	Price            float64   `json:"price"`
	PriceChange15Min float64   `json:"price_change_15min"`
	PriceChange1H    float64   `json:"price_change_1h"`
	PriceChange4H    float64   `json:"price_change_4h"`
	Volume           float64   `json:"volume"`
	VolumeRatio5     float64   `json:"volume_ratio_5"`
	VolumeRatio20    float64   `json:"volume_ratio_20"`
	VolumeTrend      float64   `json:"volume_trend"`
	RSI14            float64   `json:"rsi_14"`
	SMA5             float64   `json:"sma_5"`
	SMA10            float64   `json:"sma_10"`
	SMA20            float64   `json:"sma_20"`
	HighLowRatio     float64   `json:"high_low_ratio"`
	Volatility20     float64   `json:"volatility_20"`
	PositionInRange  float64   `json:"position_in_range"`
}

// 警报数据结构
type Alert struct {
	Type      string    `json:"type"`
	Symbol    string    `json:"symbol"`
	Value     float64   `json:"value"`
	Threshold float64   `json:"threshold"`
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
}

type Config struct {
	AlertThresholds AlertThresholds `json:"alert_thresholds"`
	UpdateInterval  int             `json:"update_interval"` // seconds
	CleanupConfig   CleanupConfig   `json:"cleanup_config"`
}

type AlertThresholds struct {
	VolumeSpike      float64 `json:"volume_spike"`
	PriceChange15Min float64 `json:"price_change_15min"`
	VolumeTrend      float64 `json:"volume_trend"`
	RSIOverbought    float64 `json:"rsi_overbought"`
	RSIOversold      float64 `json:"rsi_oversold"`
}
type CleanupConfig struct {
	InactiveTimeout   time.Duration `json:"inactive_timeout"`    // 不活跃超时时间
	MinScoreThreshold float64       `json:"min_score_threshold"` // 最低评分阈值
	NoAlertTimeout    time.Duration `json:"no_alert_timeout"`    // 无警报超时时间
	CheckInterval     time.Duration `json:"check_interval"`      // 检查间隔
}

var config = Config{
	AlertThresholds: AlertThresholds{
		VolumeSpike:      3.0,
		PriceChange15Min: 0.05,
		VolumeTrend:      2.0,
		RSIOverbought:    70,
		RSIOversold:      30,
	},
	CleanupConfig: CleanupConfig{
		InactiveTimeout:   30 * time.Minute,
		MinScoreThreshold: 15.0,
		NoAlertTimeout:    20 * time.Minute,
		CheckInterval:     5 * time.Minute,
	},
	UpdateInterval: 60, // 1 minute
}
