package frontend

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/harlow/go-micro-services/registry"
	profile "github.com/harlow/go-micro-services/services/profile/proto"
	search "github.com/harlow/go-micro-services/services/search/proto"
	"github.com/harlow/go-micro-services/tracing"
	opentracing "github.com/opentracing/opentracing-go"
)

// Server implements frontend service
type Server struct {
	searchClient  search.SearchClient
	profileClient profile.ProfileClient

	Port     int
	Tracer   opentracing.Tracer
	Registry *registry.Client
}

// Run the server
func (s *Server) Run() error {
	if s.Port == 0 {
		return fmt.Errorf("server port must be set")
	}

	if err := s.initSearchClient("srv-search"); err != nil {
		return err
	}

	if err := s.initProfileClient("srv-profile"); err != nil {
		return err
	}

	mux := tracing.NewServeMux(s.Tracer)
	mux.Handle("/", http.FileServer(http.Dir("services/frontend/static")))
	mux.Handle("/hotels", http.HandlerFunc(s.searchHandler))

	return http.ListenAndServe(fmt.Sprintf(":%d", s.Port), mux)
}

func (s *Server) initSearchClient(name string) error {
	conn, err := tracing.Dialer(name, s.Tracer, s.Registry.Client)
	if err != nil {
		return fmt.Errorf("searh dialer error: %v", err)
	}
	s.searchClient = search.NewSearchClient(conn)
	return nil
}

func (s *Server) initProfileClient(name string) error {
	conn, err := tracing.Dialer(name, s.Tracer, s.Registry.Client)
	if err != nil {
		return fmt.Errorf("profile dialer error: %v", err)
	}
	s.profileClient = profile.NewProfileClient(conn)
	return nil
}

func (s *Server) searchHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	ctx := r.Context()

	// in/out dates from query params
	inDate, outDate := r.URL.Query().Get("inDate"), r.URL.Query().Get("outDate")
	if inDate == "" || outDate == "" {
		http.Error(w, "Please specify inDate/outDate params", http.StatusBadRequest)
		return
	}

	// search for best hotels
	// TODO(hw): allow lat/lon from input params
	searchResp, err := s.searchClient.Nearby(ctx, &search.NearbyRequest{
		Lat:     37.7749,
		Lon:     -122.4194,
		InDate:  inDate,
		OutDate: outDate,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// grab locale from query params or default to en
	locale := r.URL.Query().Get("locale")
	if locale == "" {
		locale = "en"
	}

	// hotel profiles
	profileResp, err := s.profileClient.GetProfiles(ctx, &profile.Request{
		HotelIds: searchResp.HotelIds,
		Locale:   locale,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(geoJSONResponse(profileResp.Hotels))
}

// return a geoJSON response that allows google map to plot points directly on map
// https://developers.google.com/maps/documentation/javascript/datalayer#sample_geojson
func geoJSONResponse(hs []*profile.Hotel) map[string]interface{} {
	fs := []interface{}{}

	for _, h := range hs {
		fs = append(fs, map[string]interface{}{
			"type": "Feature",
			"id":   h.Id,
			"properties": map[string]string{
				"name":         h.Name,
				"phone_number": h.PhoneNumber,
			},
			"geometry": map[string]interface{}{
				"type": "Point",
				"coordinates": []float32{
					h.Address.Lon,
					h.Address.Lat,
				},
			},
		})
	}

	return map[string]interface{}{
		"type":     "FeatureCollection",
		"features": fs,
	}
}
