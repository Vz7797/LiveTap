package api

import "net/http"

// Split keeps credentials and routes separate even when both APIs share a port.
func Listeners(admin, north http.Handler, separate bool) (http.Handler, http.Handler) {
	management := http.NewServeMux()
	management.HandleFunc("/v1/", http.NotFound)
	management.Handle("/", Compress(admin))
	public := http.NewServeMux()
	public.Handle("/v1/", Compress(north))
	if separate {
		public.Handle("GET /healthz", admin)
	} else {
		public.Handle("/", management)
	}
	return public, management
}
