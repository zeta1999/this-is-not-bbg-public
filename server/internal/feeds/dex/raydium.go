package dex

import (
	"github.com/notbbg/notbbg/server/internal/bus"
)

// NewRaydiumAdapter tracks Solana DeFi tokens via DeFi Llama.
func NewRaydiumAdapter(b *bus.Bus, pollInterval interface{}) *DefiProtocolAdapter {
	return newDefiProtocolAdapter(b, "raydium", 0, []defiToken{
		{Chain: "solana", Address: "So11111111111111111111111111111111111111112", Symbol: "SOL"},
		{Chain: "solana", Address: "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v", Symbol: "USDC"},
		{Chain: "solana", Address: "Es9vMFrzaCERmJfrF4H2FYD4KCoNkY11McCe8BenwNYB", Symbol: "USDT"},
		{Chain: "solana", Address: "mSoLzYCxHdYgdzU16g5QSh3i5K3z3KZK7ytfqcJm7So", Symbol: "mSOL"},
		{Chain: "solana", Address: "4k3Dyjzvzp8eMZWUXbBCjEvwSkkk59S5iCNLY3QrkX6R", Symbol: "RAY"},
		{Chain: "solana", Address: "orcaEKTdK7LKz57vaAYr9QeNsVEPfiu6QeMU1kektZE", Symbol: "ORCA"},
	})
}
