package main

import (
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// 定义数据库模型
type StockPrice struct {
	ID        uint      `gorm:"primaryKey"`
	StockCode string    `gorm:"index"`
	StockName string    
	Price     float64   
	CreatedAt time.Time 
}

func main() {
	// 初始化数据库
	db, err := gorm.Open(sqlite.Open("stocks.db"), &gorm.Config{})
	if err != nil {
		panic("无法连接数据库")
	}
	// 自动迁移表结构
	db.AutoMigrate(&StockPrice{})

	r := gin.Default()
	r.StaticFile("/favicon.ico", "./123.ico") 
	r.GET("/stock/:code", func(c *gin.Context) {
		// 获取股票代码参数
		code := c.Param("code")

		// 验证股票代码格式（6位数字）
		if matched, _ := regexp.MatchString(`^\d{6}$`, code); !matched {
			c.JSON(http.StatusBadRequest, gin.H{"error": "股票代码必须为6位数字"})
			return
		}

		// 确定市场前缀
		var prefix string
		switch code[0] {
		case '6':
			prefix = "1."
		case '0', '3':
			prefix = "0."
		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": "不支持的股票类型"})
			return
		}

		// 构造完整API URL（添加股票名称字段f58）
		secid := prefix + code
		apiURL := "https://push2.eastmoney.com/api/qt/stock/get?" +
			"ut=fa5fd1943c7b386f172d6893dbfba10b&" +
			"invt=2&fltt=2&fields=f43,f58&secid=" + secid

		// 创建自定义HTTP请求
		client := &http.Client{}
		req, err := http.NewRequest("GET", apiURL, nil)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "创建请求失败"})
			return
		}

		// 设置请求头防止被拦截
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")
		req.Header.Set("Referer", "https://www.eastmoney.com/")

		// 发送请求
		resp, err := client.Do(req)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "API请求失败"})
			return
		}
		defer resp.Body.Close()

		// 读取响应内容
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "读取响应失败"})
			return
		}

		// 解析JSON响应
		var result map[string]interface{}
		if err := json.Unmarshal(body, &result); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "JSON解析失败"})
			return
		}

		// 检查API返回状态
		if rc, ok := result["rc"].(float64); !ok || rc != 0 {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "数据接口异常"})
			return
		}

		// 提取数据
		data, ok := result["data"].(map[string]interface{})
		if !ok {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "数据格式异常"})
			return
		}

		// 获取股票名称
		stockName, _ := data["f58"].(string)

		// 处理价格数据
		price, exists := data["f43"]
		if !exists {
			c.JSON(http.StatusOK, gin.H{"message": "该股票当前无有效价格（可能已停牌）"})
			return
		}

		var finalPrice float64
		switch v := price.(type) {
		case float64:
			finalPrice = v
		case string:
			if finalPrice, err = strconv.ParseFloat(v, 64); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "价格格式异常"})
				return
			}
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "未知价格类型"})
			return
		}

		// 保存到数据库
		record := StockPrice{
			StockCode: code,
			StockName: stockName,
			Price:     finalPrice,
		}
		if err := db.Create(&record).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "保存数据失败"})
			return
		}

		// 返回最终结果
		c.JSON(http.StatusOK, gin.H{
			"stock_code": code,
			"stock_name": stockName,
			"price":      finalPrice,
			"query_time": record.CreatedAt.Format("2006-01-02 15:04:05"),
		})
	})

	r.Run(":8080")
}