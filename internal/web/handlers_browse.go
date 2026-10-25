package web

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"sodapop/internal/catalog"
)

// browseDrinksPage renders the catalog browse grid with its filter state.
func (s *Server) browseDrinksPage(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, err := strconv.Atoi(q.Get("page"))
	if err != nil || page < 1 {
		page = 1
	}
	f := catalog.BrowseFilter{
		Style:        q.Get("style"),
		Brand:        q.Get("brand"),
		Flavour:      q.Get("flavour"),
		Status:       q.Get("status"),
		Sort:         q.Get("sort"),
		CaffeineFree: q.Get("caffeine_free") == "1",
		SugarFree:    q.Get("sugar_free") == "1",
		Page:         page,
	}
	res, err := s.Catalog.BrowseDrinks(r.Context(), f)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	total := "No sodas match these filters."
	if n := len(res.Drinks); n > 0 {
		total = fmt.Sprintf("%d sodas", n)
		if page > 1 {
			total = fmt.Sprintf("%d sodas · page %d", n, page)
		}
	}
	s.render(w, r, http.StatusOK, "browse", map[string]any{
		"Title": "Drinks",
		"Mode":  "drinks",
		"Filters": map[string]any{
			"Style":        f.Style,
			"Brand":        f.Brand,
			"Flavour":      f.Flavour,
			"Status":       f.Status,
			"Sort":         f.Sort,
			"Page":         page,
			"CaffeineFree": f.CaffeineFree,
			"SugarFree":    f.SugarFree,
		},
		"Drinks":     res.Drinks,
		"HasNext":    res.HasNext,
		"NextPage":   page + 1,
		"Count":      len(res.Drinks),
		"TotalLabel": total,
	})
}

// browseBrandsPage lists every brand with its drink count.
func (s *Server) browseBrandsPage(w http.ResponseWriter, r *http.Request) {
	brands, err := s.Catalog.ListBrands(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.render(w, r, http.StatusOK, "browse", map[string]any{
		"Title":  "Brands",
		"Mode":   "brands",
		"Brands": brands,
	})
}

// brandPage shows one brand and its drinks.
func (s *Server) brandPage(w http.ResponseWriter, r *http.Request) {
	b, err := s.Catalog.GetBrand(r.Context(), r.PathValue("slug"))
	if err != nil {
		if errors.Is(err, catalog.ErrNotFound) {
			s.render(w, r, http.StatusNotFound, "error", map[string]any{
				"Title":   "Not found",
				"Message": "That brand is not in the catalog yet.",
			})
			return
		}
		s.fail(w, r, err)
		return
	}
	s.render(w, r, http.StatusOK, "brand", map[string]any{
		"Title":       b.Name,
		"Name":        b.Name,
		"Slug":        b.Slug,
		"Description": b.Description,
		"Drinks":      b.Drinks,
		"Count":       b.Count,
	})
}

// browseStylesPage lists style families with their children.
func (s *Server) browseStylesPage(w http.ResponseWriter, r *http.Request) {
	styles, err := s.Catalog.ListStyles(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.render(w, r, http.StatusOK, "browse", map[string]any{
		"Title":  "Styles",
		"Mode":   "styles",
		"Styles": styles,
	})
}

// stylePage shows one style, its parent, children and drinks.
func (s *Server) stylePage(w http.ResponseWriter, r *http.Request) {
	st, err := s.Catalog.GetStyle(r.Context(), r.PathValue("slug"))
	if err != nil {
		if errors.Is(err, catalog.ErrNotFound) {
			s.render(w, r, http.StatusNotFound, "error", map[string]any{
				"Title":   "Not found",
				"Message": "That style is not in the catalog yet.",
			})
			return
		}
		s.fail(w, r, err)
		return
	}
	s.render(w, r, http.StatusOK, "style", map[string]any{
		"Title":       st.Name,
		"Name":        st.Name,
		"Slug":        st.Slug,
		"Description": st.Description,
		"ParentName":  st.ParentName,
		"Children":    st.Children,
		"Drinks":      st.Drinks,
		"Count":       st.Count,
	})
}

// dropsPage shows limited, seasonal and discontinued sodas.
func (s *Server) dropsPage(w http.ResponseWriter, r *http.Request) {
	drops, err := s.Catalog.BrowseDrops(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.render(w, r, http.StatusOK, "drops", map[string]any{
		"Title":        "Drops",
		"Limited":      drops.Limited,
		"Seasonal":     drops.Seasonal,
		"Discontinued": drops.Discontinued,
	})
}
