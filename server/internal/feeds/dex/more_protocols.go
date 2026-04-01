package dex

import (
	"github.com/notbbg/notbbg/server/internal/bus"
)

// NewOrcaAdapter tracks Orca (Solana AMM) tokens via DeFi Llama.
func NewOrcaAdapter(b *bus.Bus, pollInterval interface{}) *DefiProtocolAdapter {
	return newDefiProtocolAdapter(b, "orca", 0, []defiToken{
		{Chain: "solana", Address: "orcaEKTdK7LKz57vaAYr9QeNsVEPfiu6QeMU1kektZE", Symbol: "ORCA"},
		{Chain: "solana", Address: "So11111111111111111111111111111111111111112", Symbol: "SOL"},
		{Chain: "solana", Address: "mSoLzYCxHdYgdzU16g5QSh3i5K3z3KZK7ytfqcJm7So", Symbol: "mSOL"},
	})
}

// NewPancakeSwapAdapter tracks PancakeSwap (BSC) tokens via DeFi Llama.
func NewPancakeSwapAdapter(b *bus.Bus, pollInterval interface{}) *DefiProtocolAdapter {
	return newDefiProtocolAdapter(b, "pancakeswap", 0, []defiToken{
		{Chain: "bsc", Address: "0x0E09FaBB73Bd3Ade0a17ECC321fD13a19e81cE82", Symbol: "CAKE"},
		{Chain: "bsc", Address: "0xbb4CdB9CBd36B01bD1cBaEBF2De08d9173bc095c", Symbol: "WBNB"},
		{Chain: "bsc", Address: "0x55d398326f99059fF775485246999027B3197955", Symbol: "USDT"},
	})
}

// NewCurveAdapter tracks Curve Finance (Ethereum stableswap) via DeFi Llama.
func NewCurveAdapter(b *bus.Bus, pollInterval interface{}) *DefiProtocolAdapter {
	return newDefiProtocolAdapter(b, "curve", 0, []defiToken{
		{Chain: "ethereum", Address: "0xD533a949740bb3306d119CC777fa900bA034cd52", Symbol: "CRV"},
		{Chain: "ethereum", Address: "0x6B175474E89094C44Da98b954EedeAC495271d0F", Symbol: "DAI"},
		{Chain: "ethereum", Address: "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48", Symbol: "USDC"},
		{Chain: "ethereum", Address: "0xdAC17F958D2ee523a2206206994597C13D831ec7", Symbol: "USDT"},
	})
}
