package merge

// Strategy defines how conflicts should be resolved.
type Strategy string

const (
	StrategyAuto    Strategy = "auto"     // try auto-merge, fail on conflicts
	StrategyOurs    Strategy = "ours"     // keep our version on conflict
	StrategyTheirs  Strategy = "theirs"   // keep their version on conflict
	StrategyAutoLLM Strategy = "auto+llm" // try auto, then LLM on conflict
)

// Policy controls merge behavior per-space.
type Policy struct {
	Default         Strategy
	SpaceStrategies map[string]Strategy
}

// NewDefaultPolicy returns a policy that auto-merges everything.
func NewDefaultPolicy() *Policy {
	return &Policy{
		Default:         StrategyAuto,
		SpaceStrategies: make(map[string]Strategy),
	}
}

// StrategyFor returns the merge strategy for a given space.
func (p *Policy) StrategyFor(spaceID string) Strategy {
	if s, ok := p.SpaceStrategies[spaceID]; ok {
		return s
	}
	return p.Default
}

// PolicyFromConfig creates a Policy from core config values.
func PolicyFromConfig(defaultStrategy string, strategies map[string]string) *Policy {
	p := &Policy{
		Default:         Strategy(defaultStrategy),
		SpaceStrategies: make(map[string]Strategy),
	}
	if p.Default == "" {
		p.Default = StrategyAuto
	}
	for space, strat := range strategies {
		p.SpaceStrategies[space] = Strategy(strat)
	}
	return p
}
