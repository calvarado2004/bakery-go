package main

import (
	"testing"
)

func TestTemplatesLoaded(t *testing.T) {
	// initTemplates should be called by init() or at startup
	// We need to check that all expected keys are in the templates map
	
	expectedKeys := []string{
		"index",
		"order-details",
		"service",
		"product",
		"team",
		"testimonial",
		"contact",
		"404",
		// Admin
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
		// Portal
		"portal/base.html",
		"portal/dashboard.html",
		"portal/orders.html",
		"portal/order_detail.html",
		"portal/invoices.html",
		"portal/invoice_detail.html",
		"portal/login.html",
	}

	for _, key := range expectedKeys {
		if _, ok := templates[key]; !ok {
			t.Errorf("template key %q not found in templates map", key)
		}
	}

	t.Logf("Loaded %d templates: %v", len(templates), getTemplateKeys())
}

func TestStaticPageHandlerKeys(t *testing.T) {
	// Verify that staticPageHandler keys match template map keys
	staticPageKeys := []string{"service", "product", "team", "testimonial", "contact", "404"}
	for _, key := range staticPageKeys {
		if _, ok := templates[key]; !ok {
			t.Errorf("staticPageHandler(%q) would fail: template key %q not found", key, key)
		}
	}
}

func TestHandlerKeyCoverage(t *testing.T) {
	// Verify all handler template lookups have matching keys
	handlerKeys := []string{"index", "order-details"}
	for _, key := range handlerKeys {
		if _, ok := templates[key]; !ok {
			t.Errorf("handler template lookup key %q not found", key)
		}
	}
}

func getTemplateKeys() []string {
	keys := make([]string, 0, len(templates))
	for k := range templates {
		keys = append(keys, k)
	}
	return keys
}
