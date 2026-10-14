package artic

import (
	"encoding/json"

	"github.com/dctellya/artstudio/internal/ports"
)

// fields is the vendor field list requested on every call.
const fields = "id,title,artist_display,date_display,image_id"

type apiArtwork struct {
	ID            int    `json:"id"`
	Title         string `json:"title"`
	ArtistDisplay string `json:"artist_display"`
	DateDisplay   string `json:"date_display"`
	ImageID       string `json:"image_id"`
}

type apiConfig struct {
	IIIFURL string `json:"iiif_url"`
}

func (a apiArtwork) toPorts(iiif string) ports.Artwork {
	out := ports.Artwork{ID: a.ID, Title: a.Title, Artist: a.ArtistDisplay, Date: a.DateDisplay, ImageID: a.ImageID}
	if a.ImageID != "" {
		out.ImageURL = iiif + "/" + a.ImageID + "/full/843,/0/default.jpg"
	}
	return out
}

func decodeArtwork(body []byte) (ports.Artwork, error) {
	var r struct {
		Data   apiArtwork `json:"data"`
		Config apiConfig  `json:"config"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return ports.Artwork{}, err
	}
	return r.Data.toPorts(r.Config.IIIFURL), nil
}

func decodeSearch(body []byte) (ports.SearchResult, error) {
	var r struct {
		Pagination struct {
			Total       int `json:"total"`
			Limit       int `json:"limit"`
			CurrentPage int `json:"current_page"`
		} `json:"pagination"`
		Data   []apiArtwork `json:"data"`
		Config apiConfig    `json:"config"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return ports.SearchResult{}, err
	}
	res := ports.SearchResult{
		Items: make([]ports.Artwork, 0, len(r.Data)),
		Total: r.Pagination.Total, Limit: r.Pagination.Limit, Page: r.Pagination.CurrentPage,
	}
	for _, a := range r.Data {
		res.Items = append(res.Items, a.toPorts(r.Config.IIIFURL))
	}
	return res, nil
}
