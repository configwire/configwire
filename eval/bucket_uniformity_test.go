// Sequential-input uniformity/correlation gate for Bucket(number+seed).
//
// Scope: the stable-bucket gate question — whether sequential decimal-string
// inputs "0"-"9999" preserve the fnv64a uniformity through %10000. This test
// enumerates the full 10000-input space per seed (deterministic: no
// randomness, no time, no network; 10k x few seeds of fnv64a is trivial).
//
// What this test proves (and does NOT prove): it proves coarse-bin balance
// (deciles) and cross-seed independence at the 50/50-arm rate. It does NOT
// prove per-bucket uniformity (with N=10000 inputs over 10000 buckets, true
// uniformity yields ~37% empty buckets by Poisson(1) — so per-bucket exact
// checks would be wrong), and it proves nothing about k-anonymity: a passing
// gate still leaves small cohorts identifiable under low traffic. No
// uniformity/k-anonymity/anti-grind claim beyond the gate belongs in docs.
//
// Tolerance math (stated, not magic):
//   - Deciles: N=10000 inputs into 10 arms, E=1000/arm. Under uniformity each
//     arm count is Binomial(N, 0.1) with sd=sqrt(10000*0.1*0.9)=30. The bound
//     E+/-200 is ~6.7 sd; a construction that starves or doubles any arm
//     misses by 1000, far outside the bound.
//   - Chi-square over 100 bins of 100 buckets: E=100/bin, df=99, mean 99,
//     var 2*df=198, sd~=14.1. The bound chi2<200 sits ~7.2 sd above the mean.
//     Family-wise false-positive over 3 seeds x (10 deciles + 1 chi2) at
//     ~6.5+ sd per check is << 1e-6 under true uniformity.
//   - Cross-seed 50/50-arm agreement: per input, P(same arm)=0.5 under
//     independence, so agreements over N=10000 are Binomial(N,0.5) with mean
//     5000 and sd=50. The bound 5000+/-300 is 6 sd; a fully correlating
//     construction (same arm always) scores 10000, far outside.
//   - Exact-bucket cross-seed collisions: per input P(match)=1e-4 under
//     independence, count ~ Poisson(1). The bound <=10 admits ~9 sd of slack
//     (P(>10) ~= 1e-7); a seed-ignoring construction scores 10000.
package eval

import (
	"strconv"
	"testing"
)

// gateSeeds are fixed experiment-style seeds. Fixed strings keep the gate
// deterministic; three seeds give three pairwise cross-seed comparisons.
var gateSeeds = []string{"exp-seed-1", "rollout-1", "gate-seed-3"}

const (
	gateInputs    = 10000
	gateDecileLo  = 800 // E=1000, sd=30 -> +/-200 ~= 6.7 sd
	gateDecileHi  = 1200
	gateChi2Max   = 200.0 // df=99, mean 99, sd~=14.1 -> ~7.2 sd above mean
	gateAgreeLo   = 4700  // E=5000, sd=50 -> +/-300 = 6 sd
	gateAgreeHi   = 5300
	gateCollideMx = 10 // ~Poisson(1) under independence; seed-ignoring => 10000
)

// uniformityViolations runs the distribution gate for bucketFn over the full
// sequential input space per seed and returns one entry per violated bound
// (empty = pass). Parameterized on bucketFn so the red-control test can prove
// the gate rejects a clustering construction.
func uniformityViolations(bucketFn func(string, string) int, seeds []string) []string {
	var violations []string
	for _, seed := range seeds {
		deciles := make([]int, 10)
		bins := make([]int, 100)
		for i := 0; i < gateInputs; i++ {
			in := strconv.Itoa(i)
			b := bucketFn(in, seed)
			if b < 0 || b > 9999 {
				violations = append(violations, "seed "+seed+": input "+in+" out of range")
				continue
			}
			// Determinism spot check: pure function of (input, seed).
			if again := bucketFn(in, seed); again != b {
				violations = append(violations, "seed "+seed+": input "+in+" nondeterministic")
				continue
			}
			deciles[b/1000]++
			bins[b/100]++
		}
		for d, c := range deciles {
			if c < gateDecileLo || c > gateDecileHi {
				violations = append(violations, "seed "+seed+": decile "+strconv.Itoa(d)+
					" count out of bounds")
			}
		}
		chi2 := 0.0
		const want = float64(gateInputs) / 100
		for _, c := range bins {
			d := float64(c) - want
			chi2 += d * d / want
		}
		if chi2 >= gateChi2Max {
			violations = append(violations, "seed "+seed+": chi-square over bound")
		}
	}
	return violations
}

// correlationViolations runs the cross-seed gate: the same stable number must
// yield different arms across seeds at roughly the independent rate.
// Returns one entry per violated bound (empty = pass).
func correlationViolations(bucketFn func(string, string) int, seeds []string) []string {
	var violations []string
	for a := 0; a < len(seeds); a++ {
		for b := a + 1; b < len(seeds); b++ {
			agree, collide := 0, 0
			for i := 0; i < gateInputs; i++ {
				in := strconv.Itoa(i)
				ba, bb := bucketFn(in, seeds[a]), bucketFn(in, seeds[b])
				if (ba < 5000) == (bb < 5000) {
					agree++
				}
				if ba == bb {
					collide++
				}
			}
			if agree < gateAgreeLo || agree > gateAgreeHi {
				violations = append(violations, "seeds "+seeds[a]+" vs "+seeds[b]+
					": 50/50-arm agreement out of bounds")
			}
			if collide > gateCollideMx {
				violations = append(violations, "seeds "+seeds[a]+" vs "+seeds[b]+
					": exact-bucket collisions over bound")
			}
		}
	}
	return violations
}

func TestBucketSequentialUniformity(t *testing.T) {
	if v := uniformityViolations(Bucket, gateSeeds); len(v) > 0 {
		for _, e := range v {
			t.Error(e)
		}
	}
}

func TestBucketCrossSeedDecorrelation(t *testing.T) {
	if v := correlationViolations(Bucket, gateSeeds); len(v) > 0 {
		for _, e := range v {
			t.Error(e)
		}
	}
}

// TestBucketUniformityGateRedControl proves the gate above can actually go
// red: deliberately correlating constructions must produce violations. If
// this test fails, the gate is too weak to block T9 uniformity wording.
func TestBucketUniformityGateRedControl(t *testing.T) {
	// Clusters 10 sequential inputs per bucket and parks everything in the
	// first decile: decile 0 holds all 10000 inputs.
	clustered := func(n, seed string) int {
		i, _ := strconv.Atoi(n)
		return (i / 10) % 10000
	}
	if v := uniformityViolations(clustered, gateSeeds); len(v) == 0 {
		t.Error("red control: clustering stub passed the uniformity gate, gate is blind")
	}
	// Ignores the seed entirely: every cross-seed pair agrees on all inputs.
	seedBlind := func(n, seed string) int {
		i, _ := strconv.Atoi(n)
		return (i * 2654435761) % 10000
	}
	if v := correlationViolations(seedBlind, gateSeeds); len(v) == 0 {
		t.Error("red control: seed-ignoring stub passed the correlation gate, gate is blind")
	}
}
