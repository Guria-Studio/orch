// Package tunnel supervises the dashboard's optional shared URL. Two
// programs can carry it:
//
//   - cloudflared: a Cloudflare quick tunnel (`cloudflared tunnel --url …`),
//     public, needing no Cloudflare account, login or DNS — only the binary.
//   - tailscale: `tailscale serve` (reachable inside the operator's tailnet)
//     or `tailscale funnel` (public), both in the foreground, so the share
//     lives exactly as long as the child does. Needs tailscale installed and
//     logged in; funnel also needs Funnel allowed in the tailnet.
//
// autossh (Pinggy) and bore were removed: bore served plain http:// and
// Pinggy needed SSH. Either provider here serves https and needs nothing
// but its own CLI, which is what lets `tunnel.enabled: true` stay the whole
// configuration — the provider is picked when the share starts.
//
// HTTP wiring (routes, the token gate for tunnelled requests) is
// internal/dashboard's. This package exposes a Manager, the install guides
// (install.go) and the pure capability gate (capabilities.go).
package tunnel

import (
	"fmt"
	"regexp"
	"strconv"
)

// Provider ids, stable on the wire and in state.json.
const (
	ProviderCloudflared     = "cloudflared"
	ProviderTailscaleServe  = "tailscale-serve"
	ProviderTailscaleFunnel = "tailscale-funnel"
)

// Provider is the one a start without a choice uses (`orch dashboard
// --tunnel`, and every share before there was a choice).
const Provider = ProviderCloudflared

// Command is cloudflared's binary, the default provider's.
const Command = "cloudflared"

// TailscaleCommand is the binary both tailscale providers run.
const TailscaleCommand = "tailscale"

// urlPattern matches the hostname cloudflared prints once the quick tunnel
// is up ("https://seasonal-deck-organisms-sf.trycloudflare.com"). At least
// one hyphen is required: cloudflared also logs its control endpoint,
// https://api.trycloudflare.com, when a request fails, and that is not a
// URL anybody should be handed.
var urlPattern = regexp.MustCompile(`https://[a-z0-9]+(?:-[a-z0-9]+)+\.trycloudflare\.com`)

// tailscaleURLPattern matches the node's MagicDNS name tailscale prints under
// "Available within your tailnet:" / "Available on the internet:"
// ("https://box.tail1234.ts.net/"). The login.tailscale.com link it prints
// when Funnel is not allowed yet is not on ts.net, so it never matches.
var tailscaleURLPattern = regexp.MustCompile(`https://(?:[a-z0-9-]+\.)+ts\.net`)

type providerSpec struct {
	command string
	verb    string // "" for cloudflared; serve or funnel for tailscale
	url     *regexp.Regexp
}

var providers = map[string]providerSpec{
	ProviderCloudflared:     {command: Command, url: urlPattern},
	ProviderTailscaleServe:  {command: TailscaleCommand, verb: "serve", url: tailscaleURLPattern},
	ProviderTailscaleFunnel: {command: TailscaleCommand, verb: "funnel", url: tailscaleURLPattern},
}

// Providers lists every provider id, in the order a page offers them.
func Providers() []string {
	return []string{ProviderCloudflared, ProviderTailscaleServe, ProviderTailscaleFunnel}
}

// KnownProvider reports whether id names a provider.
func KnownProvider(id string) bool {
	_, ok := providers[id]
	return ok
}

// CommandFor is the binary provider runs ("" for an unknown provider).
func CommandFor(provider string) string {
	return providers[provider].command
}

// Args is cloudflared's argv (without the binary) for a quick tunnel to
// the dashboard on port. 127.0.0.1 rather than localhost: the dashboard
// binds IPv4 loopback by default and localhost may resolve to ::1 first.
func Args(port int) []string {
	return ProviderArgs(ProviderCloudflared, port)
}

// ProviderArgs is provider's argv (without the binary) for a share of the
// dashboard on port. tailscale runs in the foreground (no --bg): its serve
// config then goes away with the process, so stopping the child is the
// whole of stopping the share.
func ProviderArgs(provider string, port int) []string {
	target := "http://127.0.0.1:" + strconv.Itoa(port)
	spec := providers[provider]
	if spec.verb != "" {
		return []string{spec.verb, target}
	}
	return []string{"tunnel", "--no-autoupdate", "--url", target}
}

// providerOf is cfg's provider, the default when it names none.
func providerOf(cfg ManagerConfig) string {
	if cfg.Provider == "" {
		return Provider
	}
	return cfg.Provider
}

// commandLine assembles the full argv, refusing a port that cannot be one
// rather than spawning a tunnel to nowhere, and a provider nobody knows.
func commandLine(cfg ManagerConfig) ([]string, error) {
	if cfg.Port <= 0 || cfg.Port > 65535 {
		return nil, fmt.Errorf("tunnel: port %d is not a port", cfg.Port)
	}
	provider := providerOf(cfg)
	spec, ok := providers[provider]
	if !ok {
		return nil, fmt.Errorf("tunnel: unknown provider %q", provider)
	}
	command := cfg.Command
	if command == "" {
		command = spec.command
	}
	return append([]string{command}, ProviderArgs(provider, cfg.Port)...), nil
}
