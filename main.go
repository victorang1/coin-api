package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	binanceWSURL     = "wss://fstream.binance.com/stream"
	maxSymbolsPerWS  = 100
	telegramBotToken = "8115196480:AAFplkFLFbpP5Hkve9lPgfxHTrIAmDzDZ08"
	telegramChatID   = "5881264132"
)

var (
	activeConnections []*websocket.Conn
	result            sync.Map
)

type BinanceResponse struct {
	Stream string `json:"stream"`
	Data   struct {
		Event     string `json:"e"`
		EventTime int64  `json:"E"`
		Symbol    string `json:"s"`
		Kline     struct {
			Open     string `json:"o"`
			Close    string `json:"c"`
			IsClosed bool   `json:"x"`
		} `json:"k"`
	} `json:"data"`
}

func notifyUser(symbol string, openPrice, closePrice float64) {
	message := fmt.Sprintf("🚀 %s gained more than 10%%! Open: %.2f, Close: %.2f", symbol, openPrice, closePrice)
	sendToTelegram(message)
	log.Println(message)
}

func handleWebSocketMessage(message []byte) {
	var response BinanceResponse
	if err := json.Unmarshal(message, &response); err != nil {
		log.Println("Error parsing message:", err)
		return
	}

	if response.Data.Event == "kline" {
		candle := response.Data.Kline
		symbol := response.Data.Symbol
		openPrice, closePrice := parseFloat(candle.Open), parseFloat(candle.Close)
		if ((closePrice-openPrice)/openPrice)*100 >= 10 {
			result.Store(symbol, map[string]float64{"open": openPrice, "close": closePrice})
			notifyUser(symbol, openPrice, closePrice)
		}
	}
}

func createWebSocket(symbols []string) {
	streams := ""
	for _, symbol := range symbols {
		streams += fmt.Sprintf("%s@kline_5m/", strings.ToLower(symbol))
	}

	wsURL := fmt.Sprintf("%s?streams=%s", binanceWSURL, streams[:len(streams)-1])

	for {
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			log.Println("WebSocket error:", err)
			time.Sleep(5 * time.Second) // Wait before retrying
			continue
		}
		activeConnections = append(activeConnections, conn)

		// Reconnect every 12 hours
		resetTimer := time.AfterFunc(12*time.Hour, func() {
			log.Println("Reconnecting WebSocket after 12 hours...")
			conn.Close()
		})

		go func() {
			defer conn.Close()
			defer resetTimer.Stop()

			for {
				_, message, err := conn.ReadMessage()
				if err != nil {
					log.Println("WebSocket closed. Reconnecting...", err)
					return
				}
				handleWebSocketMessage(message)
			}
		}()

		// Keep the loop alive until a reconnection is triggered
		<-resetTimer.C
	}
}

func sendToTelegram(message string) {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", telegramBotToken)
	body, _ := json.Marshal(map[string]string{
		"chat_id": telegramChatID,
		"text":    message,
	})

	req, _ := http.NewRequest("POST", url, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Println("Telegram Error:", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Println("Telegram API Error:", resp.Status)
	}
}

func fetchTradingPairs() []string {
	resp, err := http.Get("https://fapi.binance.com/fapi/v1/exchangeInfo")
	if err != nil {
		log.Fatal("Error fetching Futures symbols:", err)
	}
	defer resp.Body.Close()

	var data struct {
		Symbols []struct {
			Symbol string `json:"symbol"`
			Status string `json:"status"`
		} `json:"symbols"`
	}
	json.NewDecoder(resp.Body).Decode(&data)

	var symbols []string
	for _, s := range data.Symbols {
		if s.Status == "TRADING" {
			symbols = append(symbols, s.Symbol)
		}
	}
	return symbols
}

func parseFloat(s string) float64 {
	value, _ := strconv.ParseFloat(s, 64)
	return value
}

func main() {
	symbols := fetchTradingPairs()
	log.Printf("Total Futures Symbols: %d", len(symbols))

	for i := 0; i < len(symbols); i += maxSymbolsPerWS {
		end := i + maxSymbolsPerWS
		if end > len(symbols) {
			end = len(symbols)
		}
		go createWebSocket(symbols[i:end])
	}

	log.Printf("Started WebSockets for real-time monitoring.")

	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()

	go func() {
		for range ticker.C {
			sendToTelegram("✅ Websocket is still running and monitoring Binance futures.")
			log.Println("Heartbeat: Bot is still running.")
		}
	}()

	select {}
}
