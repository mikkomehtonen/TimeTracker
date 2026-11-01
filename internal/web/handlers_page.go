package web

import "net/http"

func (a *App) handleIndex(w http.ResponseWriter, r *http.Request) {
	d, err := a.newPageData(r)
	if err != nil {
		internalError(w, r, err)
		return
	}
	a.render(w, r, "app.html", &d)
}
