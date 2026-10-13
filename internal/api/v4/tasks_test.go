package v4

import (
	"github.com/gorilla/mux"
	"net/http/httptest"
	"testing"
)

func TestCOO2RoutesRequireIdentityAndStaySeparate(t *testing.T) {
	router := mux.NewRouter()
	Mount(router, nil, nil)
	for _, test := range []struct{ method, path string }{{"GET", "/v4/governance"}, {"POST", "/v4/sources/preview"}, {"GET", "/v4/sources/s"}, {"GET", "/v4/tasks/t/schedule"}, {"POST", "/v4/tasks/t/schedule"}, {"POST", "/v4/tasks/t/upgrade-preview"}, {"POST", "/v4/tasks/t/archive"}, {"POST", "/v4/runs/r/replay"}, {"POST", "/v4/tables/t/export"}, {"POST", "/v4/tables/t/schema"}} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(test.method, test.path, nil))
		if response.Code != 401 {
			t.Fatalf("O4 route %s %s=%d", test.method, test.path, response.Code)
		}
	}
	for _, test := range []struct{ method, path string }{{"GET", "/v4/tasks"}, {"POST", "/v4/tasks/input-preview"}, {"POST", "/v4/tasks/save-preview"}, {"POST", "/v4/tasks/mapping-preview"}, {"GET", "/v4/runs/r/dispatches"}, {"PUT", "/v4/tasks/t/draft"}, {"POST", "/v4/tasks/t/versions"}, {"POST", "/v4/tasks/t/runs"}, {"GET", "/v4/tables"}, {"GET", "/v4/tables/t/records"}, {"GET", "/v4/records/r"}, {"GET", "/v4/runs/r/events?after=2"}, {"GET", "/v4/runs/r/observations"}, {"GET", "/v4/runs/r/work-items/i"}, {"POST", "/v4/runs/r/control"}, {"POST", "/v4/runs/r/retry-failed"}, {"POST", "/v4/runs/r/work-items/i/resolve"}, {"GET", "/v4/runs/r/work-items/i/attempts/a/commands/c"}} {
		request := httptest.NewRequest(test.method, test.path, nil)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != 401 {
			t.Fatalf("%s %s=%d", test.method, test.path, response.Code)
		}
	}
	for _, path := range []string{"/v3/tasks", "/v4/record-plans", "/v4/tasks/t/trials", "/v4/tasks/t/replays"} {
		match := new(mux.RouteMatch)
		if router.Match(httptest.NewRequest("POST", path, nil), match) && match.MatchErr == nil {
			t.Fatal("unimplemented or legacy route mounted", path)
		}
	}
}
