package web

import (
	"net/http"
	"net/url"
	"strconv"

	"sodapop/internal/integrations"
)

// deliveryStatuses is the filter order shown on the admin delivery history.
var deliveryStatuses = []struct{ Key, Label string }{
	{"", "All"},
	{integrations.DeliveryStatusDelivered, "Delivered"},
	{integrations.DeliveryStatusPending, "Pending"},
	{integrations.DeliveryStatusRetrying, "Retrying"},
	{integrations.DeliveryStatusAbandoned, "Abandoned"},
}

type deliveryStatusSummary struct {
	Key    string
	Label  string
	Count  int
	Href   string
	Active bool
}

// deliveriesPage renders the outbox delivery history with a status filter and
// per-row retry for abandoned deliveries. Every value is rendered through
// html/template's escaping; nothing here is marked as safe HTML.
func (s *Server) deliveriesPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	status := validDeliveryStatus(r.URL.Query().Get("status"))
	page := 1
	if raw := r.URL.Query().Get("page"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			page = n
		}
	}

	const perPage = 50
	rows, total, err := s.Integrations.ListDeliveries(ctx, status, perPage, (page-1)*perPage)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	counts, err := s.Integrations.DeliveryStatusCounts(ctx)
	if err != nil {
		s.fail(w, r, err)
		return
	}

	totalPages := (total + perPage - 1) / perPage
	if totalPages < 1 {
		totalPages = 1
	}

	summaries := make([]deliveryStatusSummary, 0, len(deliveryStatuses))
	for _, st := range deliveryStatuses {
		href := "/admin/deliveries"
		if st.Key != "" {
			href += "?status=" + url.QueryEscape(st.Key)
		}
		count := counts[st.Key]
		if st.Key == "" {
			for _, n := range counts {
				count += n
			}
		}
		summaries = append(summaries, deliveryStatusSummary{
			Key: st.Key, Label: st.Label, Count: count, Href: href, Active: st.Key == status,
		})
	}

	notice, errText := "", ""
	switch r.URL.Query().Get("retry") {
	case "ok":
		notice = "Delivery queued for another attempt."
	case "not_retryable":
		errText = "That delivery is not retryable. Only abandoned deliveries can be retried."
	case "error":
		errText = "The delivery could not be retried."
	}

	s.render(w, r, http.StatusOK, "admin_deliveries", map[string]any{
		"Title":      "Deliveries",
		"Status":     status,
		"Statuses":   summaries,
		"Deliveries": rows,
		"Total":      total,
		"Page":       page,
		"TotalPages": totalPages,
		"PrevHref":   deliveriesHref(status, page-1),
		"NextHref":   deliveriesHref(status, page+1),
		"HasPrev":    page > 1,
		"HasNext":    page < totalPages,
		"Notice":     notice,
		"Error":      errText,
	})
}

// deliveriesRetry resets one abandoned delivery so the worker tries it again.
func (s *Server) deliveriesRetry(w http.ResponseWriter, r *http.Request) {
	if !s.csrfOK(w, r) {
		return
	}
	status := validDeliveryStatus(r.FormValue("status"))
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id <= 0 {
		redirectDeliveries(w, r, status, "error")
		return
	}
	ok, err := s.Integrations.RetryDelivery(r.Context(), id)
	if err != nil {
		s.Log.Warn("admin delivery retry failed", "id", id, "err", err)
		redirectDeliveries(w, r, status, "error")
		return
	}
	if !ok {
		redirectDeliveries(w, r, status, "not_retryable")
		return
	}
	redirectDeliveries(w, r, status, "ok")
}

func redirectDeliveries(w http.ResponseWriter, r *http.Request, status, result string) {
	values := url.Values{"retry": {result}}
	if status != "" {
		values.Set("status", status)
	}
	http.Redirect(w, r, "/admin/deliveries?"+values.Encode(), http.StatusFound)
}

// validDeliveryStatus returns status when it is a known filter, otherwise "".
func validDeliveryStatus(status string) string {
	switch status {
	case "", integrations.DeliveryStatusDelivered, integrations.DeliveryStatusPending,
		integrations.DeliveryStatusRetrying, integrations.DeliveryStatusAbandoned:
		return status
	default:
		return ""
	}
}

// deliveriesHref builds a history link preserving the status filter.
func deliveriesHref(status string, page int) string {
	if page < 1 {
		page = 1
	}
	values := url.Values{"page": {strconv.Itoa(page)}}
	if status != "" {
		values.Set("status", status)
	}
	return "/admin/deliveries?" + values.Encode()
}
