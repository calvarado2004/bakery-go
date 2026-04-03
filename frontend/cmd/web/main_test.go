package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// staticPageHandler uses "./cmd/web/templates/..." relative to the project root
// when running as an app. In tests the CWD is the package dir (frontend/cmd/web),
// so we pass "./templates/..." directly to the handler under test.

func TestStaticPageHandler_AllRoutes(t *testing.T) {
	routes := []struct {
		name     string
		path     string
		template string
		wantText string
	}{
		{"service", "/service", "./templates/service.html", "Our Services"},
		{"product", "/product", "./templates/product.html", "Our Products"},
		{"team", "/team", "./templates/team.html", "Our Team"},
		{"testimonial", "/testimonial", "./templates/testimonial.html", "Testimonials"},
		{"contact", "/contact", "./templates/contact.html", "Contact Us"},
		{"404", "/404", "./templates/404.html", "404"},
	}

	for _, tc := range routes {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			rr := httptest.NewRecorder()

			handler := staticPageHandler(tc.template)
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

	handler := staticPageHandler("./templates/nonexistent.html")
	handler(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 for missing template, got %d", rr.Code)
	}
}

func TestStaticPageHandler_NavLinksPresent(t *testing.T) {
	pages := []struct {
		template string
	}{
		{"./templates/service.html"},
		{"./templates/product.html"},
		{"./templates/team.html"},
		{"./templates/testimonial.html"},
		{"./templates/contact.html"},
		{"./templates/404.html"},
		{"./templates/index.html"},
		{"./templates/order-details.html"},
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
		t.Run(pg.template, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			rr := httptest.NewRecorder()

			handler := staticPageHandler(pg.template)
			handler(rr, req)

			body := rr.Body.String()
			for _, link := range navLinks {
				if !strings.Contains(body, link) {
					t.Errorf("%s: missing nav link %q", pg.template, link)
				}
			}
		})
	}
}

func TestStaticPageHandler_FooterLinksPresent(t *testing.T) {
	pages := []struct {
		template string
	}{
		{"./templates/service.html"},
		{"./templates/product.html"},
		{"./templates/team.html"},
		{"./templates/testimonial.html"},
		{"./templates/contact.html"},
		{"./templates/404.html"},
		{"./templates/index.html"},
		{"./templates/order-details.html"},
	}

	footerLinks := []string{
		`href="/"`,
		`href="/service"`,
		`href="/product"`,
		`href="/team"`,
		`href="/contact"`,
	}

	for _, pg := range pages {
		t.Run(pg.template, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			rr := httptest.NewRecorder()

			handler := staticPageHandler(pg.template)
			handler(rr, req)

			body := rr.Body.String()
			for _, link := range footerLinks {
				if !strings.Contains(body, link) {
					t.Errorf("%s: missing footer link %q", pg.template, link)
				}
			}

			// No footer link should have an empty href
			if strings.Contains(body, `btn-link" href=""`) {
				t.Errorf("%s: footer contains btn-link with empty href", pg.template)
			}
		})
	}
}
