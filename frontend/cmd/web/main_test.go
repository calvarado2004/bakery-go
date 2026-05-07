package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// staticPageHandler uses the pre-parsed template map (keyed by simple names
// like "service", "product", etc.) which is populated by initTemplates at
// package init time.

func TestStaticPageHandler_AllRoutes(t *testing.T) {
	routes := []struct {
		name     string
		path     string
		key      string
		wantText string
	}{
		{"service", "/service", "service", "Our Services"},
		{"product", "/product", "product", "Our Products"},
		{"team", "/team", "team", "Our Team"},
		{"testimonial", "/testimonial", "testimonial", "Testimonials"},
		{"contact", "/contact", "contact", "Contact Us"},
		{"404", "/404", "404", "404"},
	}

	for _, tc := range routes {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			rr := httptest.NewRecorder()

			handler := staticPageHandler(tc.key)
			handler(rr, req)

			if rr.Code != http.StatusOK {
				t.Errorf("GET %s: expected status 200, got %d\nbody: %s", tc.path, rr.Code, rr.Body.String())
			}

			if !strings.Contains(rr.Body.String(), tc.wantText) {
				t.Errorf("GET %s: response body does not contain %q", tc.path, tc.wantText)
			}
		})
	}
}

func TestStaticPageHandler_MissingTemplate(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/nonexistent", nil)
	rr := httptest.NewRecorder()

	handler := staticPageHandler("nonexistent-key")
	handler(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 for missing template, got %d", rr.Code)
	}
}

func TestStaticPageHandler_NavLinksPresent(t *testing.T) {
	pages := []struct {
		key   string
		name  string
	}{
		{"service", "service"},
		{"product", "product"},
		{"team", "team"},
		{"testimonial", "testimonial"},
		{"contact", "contact"},
		{"404", "404"},
		{"index", "index"},
		{"order-details", "order-details"},
	}

	navLinks := []string{
		`href="/"`,
		`href="/orders"`,
		`href="/service"`,
		`href="/product"`,
		`href="/team"`,
		`href="/testimonial"`,
		`href="/contact"`,
	}

	for _, pg := range pages {
		t.Run(pg.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			rr := httptest.NewRecorder()

			handler := staticPageHandler(pg.key)
			handler(rr, req)

			body := rr.Body.String()
			for _, link := range navLinks {
				if !strings.Contains(body, link) {
					t.Errorf("%s: missing nav link %q", pg.name, link)
				}
			}
		})
	}
}

func TestStaticPageHandler_FooterLinksPresent(t *testing.T) {
	pages := []struct {
		key  string
		name string
	}{
		{"service", "service"},
		{"product", "product"},
		{"team", "team"},
		{"testimonial", "testimonial"},
		{"contact", "contact"},
		{"404", "404"},
		{"index", "index"},
		{"order-details", "order-details"},
	}

	footerLinks := []string{
		`href="/"`,
		`href="/service"`,
		`href="/product"`,
		`href="/team"`,
		`href="/contact"`,
	}

	for _, pg := range pages {
		t.Run(pg.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			rr := httptest.NewRecorder()

			handler := staticPageHandler(pg.key)
			handler(rr, req)

			body := rr.Body.String()
			for _, link := range footerLinks {
				if !strings.Contains(body, link) {
					t.Errorf("%s: missing footer link %q", pg.name, link)
				}
			}

			// No footer link should have an empty href
			if strings.Contains(body, `btn-link" href=""`) {
				t.Errorf("%s: footer contains btn-link with empty href", pg.name)
			}
		})
	}
}
