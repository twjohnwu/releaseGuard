package testselect

// Confidence constants for the L1/L2/L3 selective-test ladder.
//
// IMPORTANT: these are initial engineering estimates, NOT calibrated values.
// They encode the intuition "the more direct the evidence linking a change to a
// test, the higher the confidence" (file-path heuristic < coverage-map < call
// graph). The exact numbers await empirical calibration from the P0-1 HOLD
// false-positive feedback loop (see docs/decisions_log.md #18); treat them as
// tunable knobs, not ground truth.
const (
	// L1BaseConfidence is the starting confidence for the L1 file-path
	// heuristic (no DB): only same-package tests are guaranteed, so we start
	// low and subtract further for mixed-language / wide-scope diffs.
	L1BaseConfidence = 0.5

	// L2Confidence is the fixed confidence for an L2 coverage_map intersection
	// result: real coverage data, but no call-graph transitivity, so it sits
	// between the L1 heuristic and the L3 call-graph result.
	L2Confidence = 0.75

	// L3BaseConfidence is the starting confidence for the L3 reverse-BFS call
	// graph result: the strongest evidence available. Reduced when a high ratio
	// of dynamic edges makes the static graph less trustworthy.
	L3BaseConfidence = 0.9

	// L3AcceptThreshold is the minimum L3 confidence required to prefer the L3
	// result over the L1/L2 fallback. Below this, the dynamic-edge penalty has
	// eroded confidence enough that the coverage-map (L2) or heuristic (L1)
	// result is used instead.
	L3AcceptThreshold = 0.6

	// PartialStatusThreshold is the confidence below which the agent reports
	// StatusPartial rather than StatusOK. Matches L1BaseConfidence: an
	// unadjusted L1 result is on the boundary of "OK", anything the penalties
	// push below it is flagged partial.
	PartialStatusThreshold = 0.5
)
