package apperr

import "errors"

// entry pairs a domain sentinel with its Code, or with a func deriving
// interpolation args from the matched error when they depend on a runtime
// value (e.g. which resource was not found).
type entry struct {
	sentinel error
	code     Code
	args     func(err error) map[string]any
}

// Registry is an ordered allow-list of domain sentinels a boundary
// recognizes, each mapped to a Code. Build one per service at the boundary
// (HTTP handler, gRPC interceptor) — domain/usecase code never depends on
// this package.
//
// Order matters: Resolve returns the first entry matching via errors.Is, so
// register more specific sentinels before general ones when a wrapped chain
// could match several.
type Registry struct {
	entries []entry
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry { return &Registry{} }

// Register maps sentinel to a fixed Code and returns the registry for
// chaining. A nil sentinel is ignored, so it can never match every error.
func (r *Registry) Register(sentinel error, code Code) *Registry {
	if sentinel == nil {
		return r
	}
	r.entries = append(r.entries, entry{sentinel: sentinel, code: code})
	return r
}

// RegisterFunc maps sentinel to a Code plus interpolation args derived from
// the matched error, for messages that need a runtime value (a resource
// name, a field). args may be nil to behave like Register.
func (r *Registry) RegisterFunc(sentinel error, code Code, args func(err error) map[string]any) *Registry {
	if sentinel == nil {
		return r
	}
	r.entries = append(r.entries, entry{sentinel: sentinel, code: code, args: args})
	return r
}

// Resolved is the classification Resolve returns: the stable Code, the
// suggested i18n message key (KeyFor(Code)), and optional interpolation
// args for the caller's own translator.
type Resolved struct {
	Code Code
	Key  string
	Args map[string]any
}

// Resolve classifies err against every registered sentinel via errors.Is,
// returning the first match. An unmatched err (including nil) fails closed
// to CodeInternal, never echoing err's message.
func (r *Registry) Resolve(err error) Resolved {
	if err != nil {
		for _, e := range r.entries {
			if errors.Is(err, e.sentinel) {
				var args map[string]any
				if e.args != nil {
					args = e.args(err)
				}
				return Resolved{Code: e.code, Key: KeyFor(e.code), Args: args}
			}
		}
	}
	return Resolved{Code: CodeInternal, Key: KeyFor(CodeInternal)}
}
