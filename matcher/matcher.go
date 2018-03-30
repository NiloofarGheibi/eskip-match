package matcher

import (
	"fmt"
	"github.com/zalando/skipper/eskip"
	"github.com/zalando/skipper/eskipfile"
	"github.com/zalando/skipper/filters"
	"github.com/zalando/skipper/filters/builtin"
	"github.com/zalando/skipper/filters/filtertest"
	"github.com/zalando/skipper/loadbalancer"
	"github.com/zalando/skipper/logging/loggingtest"
	"github.com/zalando/skipper/predicates/cookie"
	"github.com/zalando/skipper/predicates/interval"
	"github.com/zalando/skipper/predicates/query"
	"github.com/zalando/skipper/predicates/source"
	"github.com/zalando/skipper/predicates/traffic"
	"github.com/zalando/skipper/routing"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Matcher ...
type Matcher interface {
	Test(attributes *RequestAttributes) (TestResult, error)
}

// TestResult ...
type TestResult interface {
	Route() *eskip.Route
	Request() *http.Request
	Attributes() *RequestAttributes
	PrettyPrintRoute() string
}

// RequestAttributes represents an http request to test
type RequestAttributes struct {
	Method  string
	Path    string
	Headers map[string]string
}

type matcher struct {
	routing *routing.Routing
}

type testResult struct {
	route      *eskip.Route
	req        *http.Request
	attributes *RequestAttributes
}

func (t *testResult) Route() *eskip.Route {
	return t.route
}

func (t *testResult) Request() *http.Request {
	return t.req
}

func (t *testResult) Attributes() *RequestAttributes {
	return t.attributes
}

// PrettyPrintRoute return a nice string representation of the resulting route if any
func (t *testResult) PrettyPrintRoute() string {
	if t.route == nil {
		return ""
	}

	def := t.route.Print(eskip.PrettyPrintInfo{
		Pretty:    true,
		IndentStr: "  ",
	})
	return fmt.Sprintf("%s: %s\n", t.route.Id, def)
}

// Options ...
type Options struct {
	// Path to a .eskip file defining routes
	RoutesFile string

	// CustomPredicates if any
	CustomPredicates []routing.PredicateSpec

	// CustomFilters if any
	CustomFilters []filters.Spec

	IgnoreTrailingSlash bool

	Verbose bool
}

// New create a new Matcher
func New(o *Options) (Matcher, error) {
	// creates data clients
	dataClients, err := createDataClients(o.RoutesFile)

	if err != nil {
		return nil, err
	}

	routing := createRouting(dataClients, o)

	return &matcher{
		routing,
	}, nil
}

// Test check if incoming request attributes are matching any eskip route
// Return is nil if there isn't a match
func (f *matcher) Test(attributes *RequestAttributes) (TestResult, error) {
	req, err := createHTTPRequest(attributes)

	if err != nil {
		return nil, err
	}

	// find a match
	route, _ := f.routing.Route(req)
	var eroute eskip.Route

	if route != nil {
		eroute = route.Route
	}

	if eroute.Id == "" {
		return &testResult{
			nil,
			req,
			attributes,
		}, nil
	}

	result := &testResult{
		&eroute,
		req,
		attributes,
	}

	// transform literal to pointer to use eskip.Route methods
	return result, nil
}

func createRouting(dataClients []routing.DataClient, o *Options) *routing.Routing {
	l := loggingtest.New()

	if o.Verbose == true {
		l.Unmute() // unmute skipper logging
	}

	var routingOptions routing.Options

	if o != nil {
		// create a filter registry with the available filter specs registered,
		// and register the custom filters
		registry := builtin.MakeRegistry()
		for _, f := range o.CustomFilters {
			registry.Register(f)
		}

		// create routing
		// create the proxy instance
		var mo routing.MatchingOptions
		if o.IgnoreTrailingSlash {
			mo = routing.IgnoreTrailingSlash
		}

		// include bundled custom predicates
		o.CustomPredicates = append(o.CustomPredicates,
			source.New(),
			source.NewFromLast(),
			interval.NewBetween(),
			interval.NewBefore(),
			interval.NewAfter(),
			cookie.New(),
			query.New(),
			traffic.New(),
			loadbalancer.NewGroup(),
			loadbalancer.NewMember(),
		)

		routingOptions = routing.Options{
			DataClients:     dataClients,
			Log:             l,
			FilterRegistry:  registry,
			MatchingOptions: mo,
			Predicates:      o.CustomPredicates,
		}
	} else {
		routingOptions = routing.Options{
			DataClients: dataClients,
			Log:         l,
		}
	}

	router := routing.New(routingOptions)
	defer router.Close()

	// wait for "route settings applied"
	time.Sleep(120 * time.Millisecond)

	return router
}

func createDataClients(path string) ([]routing.DataClient, error) {
	client, err := eskipfile.Open(path)
	if err != nil {
		return nil, err
	}
	DataClients := []routing.DataClient{
		client,
	}
	return DataClients, nil
}

func createHTTPRequest(attributes *RequestAttributes) (*http.Request, error) {
	if strings.HasPrefix(attributes.Path, "/") == false {
		attributes.Path = "/" + attributes.Path
	}

	u, err := url.Parse("http://localhost" + attributes.Path)
	if err != nil {
		return nil, err
	}
	if attributes.Method == "" {
		attributes.Method = "GET"
	}

	httpReq := &http.Request{
		Method: strings.ToUpper(attributes.Method),
		URL:    u,
	}
	for key, value := range attributes.Headers {
		httpReq.Header.Set(key, value)
	}
	return httpReq, nil
}

// MockFilters creates a list of mocked filters givane a list of filterNames
func MockFilters(filterNames []string) []filters.Spec {
	fs := make([]filters.Spec, len(filterNames))
	for i, filterName := range filterNames {
		fs[i] = &filtertest.Filter{
			FilterName: filterName,
		}
	}
	return fs
}
