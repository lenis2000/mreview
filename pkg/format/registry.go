package format

// Registry is the ordered list of all formatting rules. Rules run in
// registration order; Tier-1 (Safe) rules run first, then Tier-2 (PDFFix),
// then Tier-3 (Diag).
var Registry []Rule

func init() {
	registerSafeRules()
	registerPDFFixRules()
	registerDiagRules()
}
