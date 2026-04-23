// hello-world is a demo plugin that subscribes to Binance OHLC data
// and displays the latest BTC/USDT price in a styled screen tab.
package main

import (
	"encoding/json"
	"fmt"
	"time"

	sdk "github.com/notbbg/notbbg/libs/pluginsdk"
)

func main() {
	p := sdk.New("plugin.hello-world.screen")

	// Show initial screen while waiting for data.
	p.UpdateScreen("HELLO", []sdk.StyledLine{
		{Text: "  HELLO WORLD PLUGIN", Style: "header"},
		{Text: "", Style: "normal"},
		{Text: "  Waiting for BTC data...", Style: "dim"},
	})

	p.Run(func(msg sdk.Message) {
		// Parse OHLC payload.
		var payload struct {
			Instrument string  `json:"Instrument"`
			Exchange   string  `json:"Exchange"`
			Close      float64 `json:"Close"`
			High       float64 `json:"High"`
			Low        float64 `json:"Low"`
			Volume     float64 `json:"Volume"`
			Timeframe  string  `json:"Timeframe"`
		}
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			return
		}
		if payload.Instrument != "BTCUSDT" {
			return
		}

		color := "green"
		if payload.Close < 90000 {
			color = "red"
		}

		p.UpdateScreen("HELLO", []sdk.StyledLine{
			{Text: "  HELLO WORLD PLUGIN", Style: "header"},
			{Text: "", Style: "normal"},
			{Text: fmt.Sprintf("  BTC/USDT  %s  %s", payload.Exchange, payload.Timeframe), Style: "normal"},
			{Text: fmt.Sprintf("  Price:  $%.2f", payload.Close), Style: color},
			{Text: fmt.Sprintf("  High:   $%.2f", payload.High), Style: "normal"},
			{Text: fmt.Sprintf("  Low:    $%.2f", payload.Low), Style: "normal"},
			{Text: fmt.Sprintf("  Volume: %.2f", payload.Volume), Style: "dim"},
			{Text: "", Style: "normal"},
			{Text: fmt.Sprintf("  Updated: %s", time.Now().Format("15:04:05 UTC")), Style: "dim"},
			{Text: "", Style: "normal"},
			{Text: "  This plugin subscribes to ohlc.binance.* and renders", Style: "dim"},
			{Text: "  the latest BTCUSDT close price as a styled screen.", Style: "dim"},
		})
	})
}
