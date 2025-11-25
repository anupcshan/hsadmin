package handlers

import (
	"fmt"
	"net/http"
	"time"
)

// Alert types
const (
	AlertError   = "error"
	AlertSuccess = "success"
	AlertInfo    = "info"
)

// RenderAlert sends an alert message using HTMX out-of-band swap
// This allows errors and success messages to be displayed without JavaScript
// Note: Always returns 200 OK so HTMX processes the OOB swap
func RenderAlert(w http.ResponseWriter, alertType, message string, statusCode int) {
	w.Header().Set("Content-Type", "text/html")
	// Tell HTMX to not swap the main target, only process OOB swaps
	w.Header().Set("HX-Reswap", "none")
	// Always return 200 OK for HTMX to process OOB swaps
	// The statusCode parameter is kept for future use but not currently used
	w.WriteHeader(http.StatusOK)

	// Generate unique ID for the alert
	alertID := fmt.Sprintf("alert-%d", time.Now().UnixNano())

	// Icon SVG based on alert type
	var icon string
	var bgColor, borderColor, textColor string

	switch alertType {
	case AlertError:
		bgColor = "bg-red-50"
		borderColor = "border-red-200"
		textColor = "text-red-800"
		icon = `<svg class="w-5 h-5 flex-shrink-0" fill="currentColor" viewBox="0 0 20 20">
			<path fill-rule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zM8.707 7.293a1 1 0 00-1.414 1.414L8.586 10l-1.293 1.293a1 1 0 101.414 1.414L10 11.414l1.293 1.293a1 1 0 001.414-1.414L11.414 10l1.293-1.293a1 1 0 00-1.414-1.414L10 8.586 8.707 7.293z" clip-rule="evenodd"/>
		</svg>`
	case AlertSuccess:
		bgColor = "bg-green-50"
		borderColor = "border-green-200"
		textColor = "text-green-800"
		icon = `<svg class="w-5 h-5 flex-shrink-0" fill="currentColor" viewBox="0 0 20 20">
			<path fill-rule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zm3.707-9.293a1 1 0 00-1.414-1.414L9 10.586 7.707 9.293a1 1 0 00-1.414 1.414l2 2a1 1 0 001.414 0l4-4z" clip-rule="evenodd"/>
		</svg>`
	case AlertInfo:
		bgColor = "bg-blue-50"
		borderColor = "border-blue-200"
		textColor = "text-blue-800"
		icon = `<svg class="w-5 h-5 flex-shrink-0" fill="currentColor" viewBox="0 0 20 20">
			<path fill-rule="evenodd" d="M18 10a8 8 0 11-16 0 8 8 0 0116 0zm-7-4a1 1 0 11-2 0 1 1 0 012 0zM9 9a1 1 0 000 2v3a1 1 0 001 1h1a1 1 0 100-2v-3a1 1 0 00-1-1H9z" clip-rule="evenodd"/>
		</svg>`
	}

	// Render alert with HTMX out-of-band swap
	// This will be inserted into #alert-container
	// Return ONLY the OOB swap element, no main content
	html := fmt.Sprintf(`<div id="%s"
			hx-swap-oob="afterbegin:#alert-container"
			class="%s %s %s border rounded-lg shadow-lg p-4 flex items-start gap-3 animate-slide-in"
			role="alert">
			%s
			<div class="flex-grow text-sm font-medium">%s</div>
			<button onclick="this.parentElement.remove()"
				class="flex-shrink-0 text-gray-400 hover:text-gray-600">
				<svg class="w-5 h-5" fill="currentColor" viewBox="0 0 20 20">
					<path fill-rule="evenodd" d="M4.293 4.293a1 1 0 011.414 0L10 8.586l4.293-4.293a1 1 0 111.414 1.414L11.414 10l4.293 4.293a1 1 0 01-1.414 1.414L10 11.414l-4.293 4.293a1 1 0 01-1.414-1.414L8.586 10 4.293 5.707a1 1 0 010-1.414z" clip-rule="evenodd"/>
				</svg>
			</button>
		</div>
		<script>
			setTimeout(function() {
				var alert = document.getElementById('%s');
				if (alert) alert.remove();
			}, 5000);
		</script>
	`, alertID, bgColor, borderColor, textColor, icon, message, alertID)

	w.Write([]byte(html))
}

// RenderError is a helper to render error alerts with 500 status
func RenderError(w http.ResponseWriter, message string) {
	RenderAlert(w, AlertError, message, http.StatusInternalServerError)
}

// RenderErrorWithStatus is a helper to render error alerts with custom status
func RenderErrorWithStatus(w http.ResponseWriter, message string, statusCode int) {
	RenderAlert(w, AlertError, message, statusCode)
}

// RenderSuccess is a helper to render success alerts with 200 status
func RenderSuccess(w http.ResponseWriter, message string) {
	RenderAlert(w, AlertSuccess, message, http.StatusOK)
}
