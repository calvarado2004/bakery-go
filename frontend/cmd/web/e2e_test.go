package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// TestE2E_StaticPages_AllRoutes tests every static page route that is registered
// in main(). This matches exactly what the HTTP router will serve.
func TestE2E_StaticPages_AllRoutes(t *testing.T) {
	pages := []struct {
		path     string
		template string
		wantCode int
		wantText string
	}{
		{"/service", "service", http.StatusOK, "Our Services"},
		{"/product", "product", http.StatusOK, "Our Products"},
		{"/team", "team", http.StatusOK, "Our Team"},
		{"/testimonial", "testimonial", http.StatusOK, "Testimonials"},
		{"/contact", "contact", http.StatusOK, "Contact Us"},
		{"/404", "404", http.StatusOK, "404"},
	}

	for _, p := range pages {
		t.Run(p.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, p.path, nil)
			rr := httptest.NewRecorder()

			handler := staticPageHandler(p.template)
			handler(rr, req)

			if rr.Code != p.wantCode {
				t.Errorf("GET %s: expected status %d, got %d\nbody: %s",
					p.path, p.wantCode, rr.Code, truncate(rr.Body.String(), 500))
			}

			body := rr.Body.String()
			if !strings.Contains(body, p.wantText) {
				t.Errorf("GET %s: body missing %q\nbody: %s",
					p.path, p.wantText, truncate(body, 500))
			}

			// Verify critical page elements exist
			if !strings.Contains(body, "<html") {
				t.Errorf("GET %s: missing <html> tag — template may not have rendered", p.path)
			}
			if !strings.Contains(body, "Bakery Go!") {
				t.Errorf("GET %s: missing site name 'Bakery Go!'", p.path)
			}
		})
	}
}

// TestE2E_IndexPage_TemplateRender tests that the index template renders
// (it uses JS/SSE for dynamic data, so we just verify the template loads).
func TestE2E_IndexPage_TemplateRender(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()

	handler := staticPageHandler("index")
	handler(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("GET /: expected status 200, got %d\nbody: %s",
			rr.Code, truncate(rr.Body.String(), 500))
	}

	body := rr.Body.String()
	if !strings.Contains(body, "<html") {
		t.Error("GET /: missing <html> tag — index template did not render")
	}
	if !strings.Contains(body, "Bakery Go!") {
		t.Error("GET /: missing site name")
	}
}

// TestE2E_OrderDetailsPage_TemplateRender tests that the order-details template renders.
func TestE2E_OrderDetailsPage_TemplateRender(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/orders", nil)
	rr := httptest.NewRecorder()

	handler := staticPageHandler("order-details")
	handler(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("GET /orders: expected status 200, got %d\nbody: %s",
			rr.Code, truncate(rr.Body.String(), 500))
	}

	body := rr.Body.String()
	if !strings.Contains(body, "<html") {
		t.Error("GET /orders: missing <html> tag — order-details template did not render")
	}
}

// TestE2E_AllHandlerTemplateKeys tests that every route registered in main()
// has a matching template key in the templates map.
func TestE2E_AllHandlerTemplateKeys(t *testing.T) {
	// Public templates — keys used by staticPageHandler() calls in main()
	publicKeys := map[string]string{
		"index":           "index.html",
		"order-details":   "order-details.html",
		"service":         "service.html",
		"product":         "product.html",
		"team":            "team.html",
		"testimonial":     "testimonial.html",
		"contact":         "contact.html",
		"404":             "404.html",
	}
	for key, path := range publicKeys {
		if _, ok := templates[key]; !ok {
			t.Errorf("public template key %q (for %s) missing from templates map", key, path)
		}
	}

	// Admin templates
	adminKeys := []string{
		"admin/base.html",
		"admin/dashboard.html",
		"admin/bread/list.html",
		"admin/bread/form.html",
		"admin/orders/list.html",
		"admin/customers/list.html",
		"admin/customers/detail.html",
		"admin/makers/list.html",
		"admin/makers/detail.html",
		"admin/alerts.html",
		"admin/login.html",
	}
	for _, k := range adminKeys {
		if _, ok := templates[k]; !ok {
			t.Errorf("admin template key %q missing from templates map", k)
		}
	}

	// Portal templates
	portalKeys := []string{
		"portal/base.html",
		"portal/dashboard.html",
		"portal/orders.html",
		"portal/order_detail.html",
		"portal/invoices.html",
		"portal/invoice_detail.html",
		"portal/login.html",
	}
	for _, k := range portalKeys {
		if _, ok := templates[k]; !ok {
			t.Errorf("portal template key %q missing from templates map", k)
		}
	}
}

// TestE2E_StaticFiles_ExistOnDisk verifies that all expected static asset files
// exist on disk (critical for container deployment).
// Tests run from frontend/cmd/web/ so relative paths use ./templates/static/
func TestE2E_StaticFiles_ExistOnDisk(t *testing.T) {
	files := []string{
		"./templates/static/css/style.css",
		"./templates/static/js/main.js",
		"./templates/static/img/bakery.png",
		"./templates/static/img/product-1.jpg",
		"./templates/static/lib/wow/wow.min.js",
	}

	for _, f := range files {
		t.Run(f, func(t *testing.T) {
			if _, err := os.Stat(f); os.IsNotExist(err) {
				t.Errorf("static file %q does not exist on disk", f)
			}
		})
	}
}

// TestE2E_StaticFileContent verifies static files contain actual content,
// not error messages or truncated data.
func TestE2E_StaticFileContent(t *testing.T) {
	cssContent, err := os.ReadFile("./templates/static/css/style.css")
	if err != nil {
		t.Fatalf("Failed to read CSS: %v", err)
	}
	if len(cssContent) < 1000 {
		t.Errorf("CSS file too short (%d bytes) — may be serving error page", len(cssContent))
	}
	if strings.Contains(string(cssContent), "Template not found") {
		t.Error("CSS content contains 'Template not found' — serving broken")
	}

	jsContent, err := os.ReadFile("./templates/static/js/main.js")
	if err != nil {
		t.Fatalf("Failed to read JS: %v", err)
	}
	if len(jsContent) < 100 {
		t.Errorf("JS file too short (%d bytes) — may be serving error page", len(jsContent))
	}
	if strings.Contains(string(jsContent), "Template not found") {
		t.Error("JS content contains 'Template not found' — serving broken")
	}
}

// TestE2E_StaticFileServingViaHandler verifies that static files are served
// correctly through the http.FileServer handler (same mechanism used in production).
func TestE2E_StaticFileServingViaHandler(t *testing.T) {
	// Use the same file server path as in production (relative to test CWD)
	fs := http.FileServer(http.Dir("./templates/static"))

	testCases := []struct {
		path string
		size int
	}{
		{"/css/style.css", 1000},
		{"/js/main.js", 100},
	}

	for _, tc := range testCases {
		t.Run(tc.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/static"+tc.path, nil)
			rr := httptest.NewRecorder()

			// Apply StripPrefix just like in production
			handler := http.StripPrefix("/static/", fs)
			handler.ServeHTTP(rr, req)

			if rr.Code != http.StatusOK {
				t.Errorf("GET /static%s: expected 200, got %d", tc.path, rr.Code)
			}

			body, err := io.ReadAll(rr.Body)
			if err != nil {
				t.Fatalf("Failed to read response: %v", err)
			}

			if len(body) < tc.size {
				t.Errorf("GET /static%s: expected at least %d bytes, got %d",
					tc.path, tc.size, len(body))
			}
		})
	}
}

// TestE2E_NavLinksPresent verifies that navigation links are present on all pages.
func TestE2E_NavLinksPresent(t *testing.T) {
	pages := []struct {
		template string
		name     string
	}{
		{"index", "index"},
		{"service", "service"},
		{"product", "product"},
		{"team", "team"},
		{"testimonial", "testimonial"},
		{"contact", "contact"},
		{"404", "404"},
	}

	expectedLinks := []string{`href="/"`, `href="/service"`, `href="/product"`, `href="/contact"`}

	for _, p := range pages {
		t.Run(p.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/"+p.name, nil)
			rr := httptest.NewRecorder()

			handler := staticPageHandler(p.template)
			handler(rr, req)

			body := rr.Body.String()
			for _, link := range expectedLinks {
				if !strings.Contains(body, link) {
					t.Errorf("Page %s missing nav link %s", p.name, link)
				}
			}
		})
	}
}

// --- helpers ---

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
