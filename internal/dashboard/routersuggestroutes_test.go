package dashboard

import (
	"net/http"
	"testing"
)

// The demo's history fills the Usage page's route table (#457).
func TestRouterSuggestAgainstTheDemoProject(t *testing.T) {
	s := demoServer(t)
	var got routerSuggestResponse
	decode(t, get(t, s, "/api/router/suggest?period=all"), &got)
	if got.Period != "all" || got.MinSample == 0 || len(got.Kinds) == 0 || len(got.Kinds[0].Routes) == 0 {
		t.Errorf("router suggest = %+v", got)
	}
	if resp := get(t, s, "/api/router/suggest?period=1y"); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("period=1y: status %d, want 400", resp.StatusCode)
	}
}
