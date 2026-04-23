// monitor is a read-only plugin that displays a live dashboard of all data
// feeds: message rates, latest prices, feed health. Demonstrates a pure
// read-only cell grid plugin with no input cells.
package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	sdk "github.com/notbbg/notbbg/libs/pluginsdk"
)

type feedInfo struct {
	topic     string
	exchange  string
	ticker    string
	lastPrice float64
	msgCount  int64
	lastSeen  time.Time
}

type state struct {
	feeds      map[string]*feedInfo // topic → feed
	totalMsgs  int64
	startTime  time.Time
}

func main() {
	p := sdk.New("plugin.monitor.screen")
	s := state{
		feeds:     make(map[string]*feedInfo),
		startTime: time.Now(),
	}

	p.Run(func(msg sdk.Message) {
		s.totalMsgs++

		// Parse common fields.
		var payload struct {
			Instrument string  `json:"Instrument"`
			Exchange   string  `json:"Exchange"`
			Close      float64 `json:"Close"`
			Price      float64 `json:"Price"`
		}
		json.Unmarshal(msg.Payload, &payload)

		key := msg.Topic
		fi, ok := s.feeds[key]
		if !ok {
			fi = &feedInfo{topic: key, exchange: payload.Exchange, ticker: payload.Instrument}
			s.feeds[key] = fi
		}
		fi.msgCount++
		fi.lastSeen = time.Now()
		if payload.Close > 0 {
			fi.lastPrice = payload.Close
		} else if payload.Price > 0 {
			fi.lastPrice = payload.Price
		}

		// Throttle grid updates to every 50 messages.
		if s.totalMsgs%50 == 0 {
			p.UpdateCellGrid("FEEDS", buildGrid(s), true)
		}
	})
}

func buildGrid(s state) []sdk.Cell {
	uptime := time.Since(s.startTime).Truncate(time.Second)
	rate := float64(s.totalMsgs) / time.Since(s.startTime).Seconds()

	cells := []sdk.Cell{
		sdk.HeaderCell(0, 0, "FEED MONITOR"),
		sdk.NumberCell(0, 1, "Total Msgs", float64(s.totalMsgs), 0, ""),
		sdk.NumberCell(0, 2, "Rate", rate, 1, "/s"),
		sdk.TextCell(0, 3, fmt.Sprintf("Uptime: %s", uptime), &sdk.CellStyle{Fg: "dim"}),
	}

	// Sort feeds by exchange+ticker for stable display.
	type entry struct {
		key string
		fi  *feedInfo
	}
	var sorted []entry
	for k, fi := range s.feeds {
		sorted = append(sorted, entry{k, fi})
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].key < sorted[j].key })

	// Aggregate by exchange.
	exchanges := make(map[string]struct {
		count   int
		msgs    int64
		tickers []string
	})
	for _, e := range sorted {
		ex := e.fi.exchange
		if ex == "" {
			ex = "unknown"
		}
		agg := exchanges[ex]
		agg.count++
		agg.msgs += e.fi.msgCount
		if len(agg.tickers) < 5 {
			agg.tickers = append(agg.tickers, e.fi.ticker)
		}
		exchanges[ex] = agg
	}

	// Exchange summary table.
	cells = append(cells, sdk.SectionCell(2, 0, "── EXCHANGES ──", 5))
	cells = append(cells,
		sdk.Cell{Address: sdk.CellAddress{Row: 3, Col: 0}, Type: "text", Text: "Exchange", Style: &sdk.CellStyle{Bold: true, Fg: "dim"}},
		sdk.Cell{Address: sdk.CellAddress{Row: 3, Col: 1}, Type: "text", Text: "Feeds", Style: &sdk.CellStyle{Bold: true, Fg: "dim"}},
		sdk.Cell{Address: sdk.CellAddress{Row: 3, Col: 2}, Type: "text", Text: "Messages", Style: &sdk.CellStyle{Bold: true, Fg: "dim"}},
		sdk.Cell{Address: sdk.CellAddress{Row: 3, Col: 3}, Type: "text", Text: "Tickers", Style: &sdk.CellStyle{Bold: true, Fg: "dim"}},
	)

	var exNames []string
	for ex := range exchanges {
		exNames = append(exNames, ex)
	}
	sort.Strings(exNames)

	row := uint32(4)
	for _, ex := range exNames {
		agg := exchanges[ex]
		tickers := ""
		for i, t := range agg.tickers {
			if i > 0 {
				tickers += ", "
			}
			tickers += t
		}
		if agg.count > len(agg.tickers) {
			tickers += fmt.Sprintf(" (+%d)", agg.count-len(agg.tickers))
		}
		cells = append(cells,
			sdk.TextCell(row, 0, ex, &sdk.CellStyle{Fg: "cyan"}),
			sdk.NumberCell(row, 1, "", float64(agg.count), 0, ""),
			sdk.NumberCell(row, 2, "", float64(agg.msgs), 0, ""),
			sdk.TextCell(row, 3, tickers, nil),
		)
		row++
	}

	// Top feeds by message count.
	row += 1
	cells = append(cells, sdk.SectionCell(row, 0, "── TOP FEEDS (by volume) ──", 5))
	row++
	cells = append(cells,
		sdk.Cell{Address: sdk.CellAddress{Row: row, Col: 0}, Type: "text", Text: "Topic", Style: &sdk.CellStyle{Bold: true, Fg: "dim"}},
		sdk.Cell{Address: sdk.CellAddress{Row: row, Col: 1}, Type: "text", Text: "Last Price", Style: &sdk.CellStyle{Bold: true, Fg: "dim"}},
		sdk.Cell{Address: sdk.CellAddress{Row: row, Col: 2}, Type: "text", Text: "Messages", Style: &sdk.CellStyle{Bold: true, Fg: "dim"}},
		sdk.Cell{Address: sdk.CellAddress{Row: row, Col: 3}, Type: "text", Text: "Last Seen", Style: &sdk.CellStyle{Bold: true, Fg: "dim"}},
	)
	row++

	// Sort by message count descending, show top 15.
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].fi.msgCount > sorted[j].fi.msgCount })
	limit := 15
	if len(sorted) < limit {
		limit = len(sorted)
	}
	for _, e := range sorted[:limit] {
		ago := time.Since(e.fi.lastSeen).Truncate(time.Second)
		agoStyle := &sdk.CellStyle{Fg: "green"}
		if ago > 30*time.Second {
			agoStyle = &sdk.CellStyle{Fg: "yellow"}
		}
		if ago > 2*time.Minute {
			agoStyle = &sdk.CellStyle{Fg: "red"}
		}

		// Shorten topic for display.
		topic := e.key
		if len(topic) > 25 {
			topic = topic[:22] + "..."
		}

		cells = append(cells,
			sdk.TextCell(row, 0, topic, nil),
		)
		if e.fi.lastPrice > 0 {
			cells = append(cells, sdk.NumberCell(row, 1, "", e.fi.lastPrice, 2, "$"))
		} else {
			cells = append(cells, sdk.TextCell(row, 1, "---", &sdk.CellStyle{Fg: "dim"}))
		}
		cells = append(cells,
			sdk.NumberCell(row, 2, "", float64(e.fi.msgCount), 0, ""),
			sdk.TextCell(row, 3, fmt.Sprintf("%s ago", ago), agoStyle),
		)
		row++
	}

	return cells
}
