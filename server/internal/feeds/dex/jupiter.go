package dex

import (
	"github.com/notbbg/notbbg/server/internal/bus"
)

// NewJupiterAdapter tracks Solana DEX tokens via DeFi Llama.
// Jupiter's own API (price.jup.ag) is unreliable/requires auth.
func NewJupiterAdapter(b *bus.Bus, pollInterval interface{}) *DefiProtocolAdapter {
	return newDefiProtocolAdapter(b, "jupiter", 0, []defiToken{
		{Chain: "solana", Address: "JUPyiwrYJFskUPiHa7hkeR8VUtAeFoSYbKedZNsDvCN", Symbol: "JUP"},
		{Chain: "solana", Address: "So11111111111111111111111111111111111111112", Symbol: "SOL"},
		{Chain: "solana", Address: "7dHbWXmci3dT8UFYWYZweBLXgycu7Y3iL6trKn1Y7ARj", Symbol: "STEP"},
		{Chain: "solana", Address: "SHDWyBxihqiCj6YekG2GUr7wqKLeLAMK1gHZck9pL6y", Symbol: "SHDW"},
	})
}
