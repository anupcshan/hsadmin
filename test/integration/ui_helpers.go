package integration

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-rod/rod"
	"github.com/stretchr/testify/require"
)

// Default timeouts for UI operations
const (
	DefaultClickTimeout    = 15 * time.Second
	DefaultWaitTimeout     = 15 * time.Second
	DefaultPollInterval    = 100 * time.Millisecond
	DefaultSSEPollInterval = 1 * time.Second

	// QuickElementTimeout is used by QuickElement to fail fast.
	// See QuickElement documentation for details.
	QuickElementTimeout = 500 * time.Millisecond
)

// QuickElement finds an element with a short timeout, designed for use in retry loops.
//
// rod's page.Element() internally waits and retries until it finds a matching element,
// using the page's default timeout (often 30+ seconds). This is problematic when used
// inside a require.Eventually() retry loop:
//
//	require.Eventually(t, func() bool {
//	    btn, _ := page.Element(selector)  // Blocks for 30s if not found!
//	    btn.Click()
//	    return checkSuccess()
//	}, 15*time.Second, 50*time.Millisecond)  // Only 1 attempt possible
//
// With a 30-second internal timeout, a single failed Element() call consumes the entire
// 15-second Eventually budget, allowing only one attempt instead of many.
//
// QuickElement solves this by using a short 500ms timeout:
//
//	require.Eventually(t, func() bool {
//	    btn, _ := QuickElement(page, selector)  // Fails fast after 500ms
//	    btn.Click()
//	    return checkSuccess()
//	}, 15*time.Second, 50*time.Millisecond)  // ~30 attempts possible
//
// Use QuickElement inside retry loops. Use page.Element() or page.MustElement()
// for one-shot operations where blocking is acceptable.
func QuickElement(page *rod.Page, selector string) (*rod.Element, error) {
	return page.Timeout(QuickElementTimeout).Element(selector)
}

// QuickElementR is like QuickElement but matches by regex on element text.
// See QuickElement for why this exists.
func QuickElementR(page *rod.Page, selector, regex string) (*rod.Element, error) {
	return page.Timeout(QuickElementTimeout).ElementR(selector, regex)
}

// ClickElement clicks an element by selector, retrying if SSE updates cause stale references.
// This is the primary way to click elements in tests that have SSE updates.
func ClickElement(t *testing.T, page *rod.Page, selector string) {
	t.Helper()
	ClickElementWithTimeout(t, page, selector, DefaultClickTimeout)
}

// ClickElementWithTimeout clicks an element with a custom timeout.
func ClickElementWithTimeout(t *testing.T, page *rod.Page, selector string, timeout time.Duration) {
	t.Helper()
	require.Eventually(t, func() bool {
		elem, err := page.Element(selector)
		if err != nil || elem == nil {
			return false
		}
		return elem.Click("left", 1) == nil
	}, timeout, DefaultPollInterval, "Failed to click element: "+selector)
}

// ClickElementInRow clicks an element within a specific row, retrying for SSE updates.
// Useful when there are multiple similar elements on the page (e.g., menu buttons for different machines).
func ClickElementInRow(t *testing.T, page *rod.Page, rowSelector, elementSelector string) {
	t.Helper()
	require.Eventually(t, func() bool {
		row, err := page.Element(rowSelector)
		if err != nil || row == nil {
			return false
		}
		elem, err := row.Element(elementSelector)
		if err != nil || elem == nil {
			return false
		}
		return elem.Click("left", 1) == nil
	}, DefaultClickTimeout, DefaultPollInterval, "Failed to click "+elementSelector+" in row "+rowSelector)
}

// ClickElementInRowByText clicks an element within a row that matches text, retrying for SSE updates.
func ClickElementInRowByText(t *testing.T, page *rod.Page, rowText, elementSelector string) {
	t.Helper()
	require.Eventually(t, func() bool {
		row, err := page.ElementR("tr", rowText)
		if err != nil || row == nil {
			return false
		}
		elem, err := row.Element(elementSelector)
		if err != nil || elem == nil {
			return false
		}
		return elem.Click("left", 1) == nil
	}, DefaultClickTimeout, DefaultPollInterval, "Failed to click "+elementSelector+" in row containing '"+rowText+"'")
}

// WaitForVisible waits for an element to become visible, handling SSE updates.
func WaitForVisible(t *testing.T, page *rod.Page, selector string) {
	t.Helper()
	WaitForVisibleWithTimeout(t, page, selector, DefaultWaitTimeout)
}

// WaitForVisibleWithTimeout waits for an element to become visible with a custom timeout.
func WaitForVisibleWithTimeout(t *testing.T, page *rod.Page, selector string, timeout time.Duration) {
	t.Helper()
	require.Eventually(t, func() bool {
		elem, err := page.Element(selector)
		if err != nil || elem == nil {
			return false
		}
		visible, err := elem.Visible()
		return err == nil && visible
	}, timeout, DefaultPollInterval, "Element not visible: "+selector)
}

// GetElements safely gets elements, returning empty slice on error (no panic).
// Use this inside require.Eventually callbacks instead of MustElements.
func GetElements(page *rod.Page, selector string) []*rod.Element {
	elems, err := page.Elements(selector)
	if err != nil {
		return nil
	}
	return elems
}

// GetElementHTML safely gets an element's HTML, returning empty string on error.
// Use this inside require.Eventually callbacks instead of MustHTML.
func GetElementHTML(elem *rod.Element) string {
	if elem == nil {
		return ""
	}
	html, err := elem.HTML()
	if err != nil {
		return ""
	}
	return html
}

// CountElements safely counts elements matching a selector.
func CountElements(page *rod.Page, selector string) int {
	return len(GetElements(page, selector))
}

// WaitForElementCount waits until the page has exactly the expected number of elements.
func WaitForElementCount(t *testing.T, page *rod.Page, selector string, expectedCount int, timeout time.Duration) {
	t.Helper()
	require.Eventually(t, func() bool {
		return CountElements(page, selector) == expectedCount
	}, timeout, DefaultSSEPollInterval, "Expected %d elements matching '%s'", expectedCount, selector)
}

// WaitForElementToContainText waits for any element matching selector to contain the text.
func WaitForElementToContainText(t *testing.T, page *rod.Page, selector, text string, timeout time.Duration) {
	t.Helper()
	require.Eventually(t, func() bool {
		elems := GetElements(page, selector)
		for _, elem := range elems {
			if html := GetElementHTML(elem); html != "" {
				if strings.Contains(html, text) {
					return true
				}
			}
		}
		return false
	}, timeout, DefaultSSEPollInterval, "No element '%s' contains text '%s'", selector, text)
}

// WaitForElementToDisappear waits for elements containing specific text to disappear.
func WaitForElementToDisappear(t *testing.T, page *rod.Page, selector, text string, timeout time.Duration) {
	t.Helper()
	require.Eventually(t, func() bool {
		elems := GetElements(page, selector)
		for _, elem := range elems {
			if html := GetElementHTML(elem); html != "" {
				if strings.Contains(html, text) {
					return false // Still exists
				}
			}
		}
		return true // Gone
	}, timeout, DefaultSSEPollInterval, "Element '%s' containing '%s' should disappear", selector, text)
}

// WaitForURL waits for the page URL to match the expected URL.
func WaitForURL(t *testing.T, page *rod.Page, expectedURL string, timeout time.Duration) {
	t.Helper()
	require.Eventually(t, func() bool {
		info, err := page.Info()
		if err != nil {
			return false
		}
		return info.URL == expectedURL
	}, timeout, DefaultPollInterval, "Expected URL: "+expectedURL)
}

// OpenAndClickDropdownItem opens a dropdown, clicks an item, and verifies the expected outcome.
// Retries the entire sequence if any step fails or the expected element doesn't appear.
// expectedSelector specifies what should appear after clicking (e.g., "dialog[open]" for a modal).
func OpenAndClickDropdownItem(t *testing.T, page *rod.Page, menuButtonSelector, itemSelector, expectedSelector string) {
	t.Helper()
	attempts := 0
	require.Eventually(t, func() bool {
		attempts++
		t.Logf("Attempt %d: starting", attempts)

		// Click to open dropdown
		btn, err := QuickElement(page, menuButtonSelector)
		if err != nil || btn == nil {
			t.Logf("Attempt %d: button not found", attempts)
			return false
		}
		t.Logf("Attempt %d: found button, clicking", attempts)
		if btn.Click("left", 1) != nil {
			t.Logf("Attempt %d: button click failed", attempts)
			return false
		}
		t.Logf("Attempt %d: button clicked", attempts)

		// Click the item in the open dropdown
		item, err := QuickElement(page, "details[open] "+itemSelector)
		if err != nil || item == nil {
			t.Logf("Attempt %d: item not found in open dropdown", attempts)
			return false
		}
		t.Logf("Attempt %d: found item, clicking", attempts)
		if item.Click("left", 1) != nil {
			t.Logf("Attempt %d: item click failed", attempts)
			return false
		}
		t.Logf("Attempt %d: item clicked", attempts)

		// Verify expected element appeared
		expected, err := QuickElement(page, expectedSelector)
		if err != nil || expected == nil {
			t.Logf("Attempt %d: expected element not found: %s", attempts, expectedSelector)
			return false
		}
		visible, _ := expected.Visible()
		if !visible {
			t.Logf("Attempt %d: expected element not visible: %s", attempts, expectedSelector)
			return false
		}

		t.Logf("Attempt %d: SUCCESS", attempts)
		return true
	}, DefaultClickTimeout, 50*time.Millisecond, "Failed to open dropdown, click "+itemSelector+", and see "+expectedSelector)
}

// OpenAndClickDropdownItemInRowByText opens a dropdown in a specific row, clicks an item, and verifies the expected outcome.
// The row is found by matching text content. Retries the entire sequence if any step fails.
// expectedSelector specifies what should appear after clicking (e.g., "dialog[open]" for a modal).
func OpenAndClickDropdownItemInRowByText(t *testing.T, page *rod.Page, rowText, menuButtonSelector, itemSelector, expectedSelector string) {
	t.Helper()
	attempts := 0
	require.Eventually(t, func() bool {
		attempts++
		t.Logf("Attempt %d: starting", attempts)

		// Find the row containing the text
		row, err := QuickElementR(page, "tr", rowText)
		if err != nil || row == nil {
			t.Logf("Attempt %d: row not found for text '%s'", attempts, rowText)
			return false
		}
		t.Logf("Attempt %d: found row", attempts)

		// Click the menu button in this row
		btn, err := row.Element(menuButtonSelector)
		if err != nil || btn == nil {
			t.Logf("Attempt %d: button not found in row", attempts)
			return false
		}
		t.Logf("Attempt %d: found button, clicking", attempts)

		if btn.Click("left", 1) != nil {
			t.Logf("Attempt %d: button click failed", attempts)
			return false
		}
		t.Logf("Attempt %d: button clicked", attempts)

		// Click the item in the open dropdown
		item, err := QuickElement(page, "details[open] "+itemSelector)
		if err != nil || item == nil {
			t.Logf("Attempt %d: item not found in open dropdown", attempts)
			return false
		}
		t.Logf("Attempt %d: found item, clicking", attempts)

		if item.Click("left", 1) != nil {
			t.Logf("Attempt %d: item click failed", attempts)
			return false
		}
		t.Logf("Attempt %d: item clicked", attempts)

		// Verify expected element appeared
		expected, err := QuickElement(page, expectedSelector)
		if err != nil || expected == nil {
			t.Logf("Attempt %d: expected element not found: %s", attempts, expectedSelector)
			return false
		}
		visible, _ := expected.Visible()
		if !visible {
			t.Logf("Attempt %d: expected element not visible: %s", attempts, expectedSelector)
			return false
		}

		t.Logf("Attempt %d: SUCCESS", attempts)
		return true
	}, DefaultClickTimeout, 50*time.Millisecond, "Failed to open dropdown in row '"+rowText+"', click "+itemSelector+", and see "+expectedSelector)
}

// ClickAndWaitForModalClose clicks a button and waits for a modal to close.
// Uses WaitRequestIdle to wait for HTMX network requests to complete.
// Retries if the click was on a stale element (modal doesn't close).
func ClickAndWaitForModalClose(t *testing.T, page *rod.Page, buttonSelector, modalSelector string) {
	t.Helper()
	attempts := 0
	require.Eventually(t, func() bool {
		attempts++

		// Set up request idle wait BEFORE clicking
		wait := page.WaitRequestIdle(500*time.Millisecond, nil, nil, nil)

		// Find and click button fresh
		btn, err := page.Element(buttonSelector)
		if err != nil || btn == nil {
			t.Logf("Attempt %d: button not found: %s", attempts, buttonSelector)
			return false
		}
		if btn.Click("left", 1) != nil {
			t.Logf("Attempt %d: click failed", attempts)
			return false
		}

		// Wait for network requests to complete
		wait()

		// Check if modal closed
		modal, _ := page.Element(modalSelector)
		if modal == nil {
			t.Logf("Attempt %d: SUCCESS (modal gone)", attempts)
			return true
		}
		visible, _ := modal.Visible()
		if !visible {
			t.Logf("Attempt %d: SUCCESS (modal hidden)", attempts)
			return true
		}

		t.Logf("Attempt %d: modal still visible, retrying", attempts)
		return false
	}, 15*time.Second, 100*time.Millisecond, "Failed to click "+buttonSelector+" and close "+modalSelector)
}

// ScreenshotOnFailure captures a screenshot when a test fails.
// Call this in a defer at the start of your test, passing the page you want to screenshot.
// Usage: defer ScreenshotOnFailure(t, page)
func ScreenshotOnFailure(t *testing.T, page *rod.Page) {
	t.Helper()
	if !t.Failed() {
		return
	}

	// Create screenshots directory
	screenshotDir := filepath.Join(".testoutput", "screenshots")
	if err := os.MkdirAll(screenshotDir, 0755); err != nil {
		t.Logf("Failed to create screenshot directory: %v", err)
		return
	}

	// Generate filename from test name
	safeName := strings.ReplaceAll(t.Name(), "/", "_")
	filename := filepath.Join(screenshotDir, fmt.Sprintf("%s_%d.png", safeName, time.Now().UnixNano()))

	// Capture screenshot
	data, err := page.Screenshot(true, nil)
	if err != nil {
		t.Logf("Failed to capture screenshot: %v", err)
		return
	}

	// Write to file
	if err := os.WriteFile(filename, data, 0644); err != nil {
		t.Logf("Failed to write screenshot: %v", err)
		return
	}

	t.Logf("Screenshot saved to: %s", filename)
}

// SetupPageWithScreenshot creates a page and registers screenshot capture on failure.
// Returns the page. The page will be closed automatically on test cleanup.
func SetupPageWithScreenshot(t *testing.T, browser *rod.Browser, url string) *rod.Page {
	t.Helper()
	page := browser.MustPage(url)
	t.Cleanup(func() {
		ScreenshotOnFailure(t, page)
		page.MustClose()
	})
	page.MustWaitLoad()
	return page
}

// IsDropdownOpen checks if a dropdown (details element) is open.
// Returns true if open, false otherwise (including on error).
func IsDropdownOpen(page *rod.Page, detailsSelector string) bool {
	details, err := page.Element(detailsSelector)
	if err != nil || details == nil {
		return false
	}
	prop, err := details.Property("open")
	if err != nil {
		return false
	}
	return prop.Bool()
}

// WaitForDropdownClosed waits for a dropdown to close.
func WaitForDropdownClosed(t *testing.T, page *rod.Page, detailsSelector string, timeout time.Duration) {
	t.Helper()
	require.Eventually(t, func() bool {
		return !IsDropdownOpen(page, detailsSelector)
	}, timeout, DefaultPollInterval, "Dropdown should close: "+detailsSelector)
}

// WaitForDropdownOpen waits for a dropdown to open.
func WaitForDropdownOpen(t *testing.T, page *rod.Page, detailsSelector string, timeout time.Duration) {
	t.Helper()
	require.Eventually(t, func() bool {
		return IsDropdownOpen(page, detailsSelector)
	}, timeout, DefaultPollInterval, "Dropdown should open: "+detailsSelector)
}
