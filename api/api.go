package api

import (
	"encoding/json"
	"fmt"
	"github.com/gorilla/context"
	"github.com/gorilla/mux"
	"github.com/martinp/atbapi/atb"
	"github.com/pmylund/go-cache"
	"log"
	"net/http"
	"strconv"
	"time"
)

type Api struct {
	Client atb.Client
	cache  *cache.Cache
	expiration
}

type expiration struct {
	departures time.Duration
	stops      time.Duration
}

func marshal(data interface{}, indent bool) ([]byte, error) {
	if indent {
		return json.MarshalIndent(data, "", "  ")
	}
	return json.Marshal(data)
}

func (a *Api) getBusStops() (BusStops, error) {
	const cacheKey = "stops"
	cached, ok := a.cache.Get(cacheKey)
	if ok {
		cachedBusStops, ok := cached.(BusStops)
		if !ok {
			return BusStops{}, fmt.Errorf(
				"type assertion of cached value failed")
		}
		return cachedBusStops, nil
	}
	atbBusStops, err := a.Client.GetBusStops()
	if err != nil {
		return BusStops{}, err
	}
	busStops, err := convertBusStops(atbBusStops)
	if err != nil {
		return BusStops{}, err
	}
	// Create a map of nodeIds
	busStops.nodeIds = make(map[int]struct{}, len(busStops.Stops))
	for _, s := range busStops.Stops {
		busStops.nodeIds[s.NodeId] = struct{}{}
	}
	a.cache.Set(cacheKey, busStops, a.expiration.stops)
	return busStops, nil
}

func (a *Api) getDepartures(nodeId int) (Departures, error) {
	cacheKey := strconv.Itoa(nodeId)
	cached, ok := a.cache.Get(cacheKey)
	if ok {
		cachedDepartures, ok := cached.(Departures)
		if !ok {
			return Departures{}, fmt.Errorf(
				"type assertion of cached value failed")
		}
		return cachedDepartures, nil
	}
	forecasts, err := a.Client.GetRealTimeForecast(nodeId)
	if err != nil {
		return Departures{}, err
	}
	departures, err := convertForecasts(forecasts)
	if err != nil {
		return Departures{}, err
	}
	a.cache.Set(cacheKey, departures, cache.DefaultExpiration)
	return departures, nil
}

func (a *Api) BusStopsHandler(w http.ResponseWriter, req *http.Request) *Error {
	busStops, err := a.getBusStops()
	if err != nil {
		log.Print(err)
		return &Error{
			error:   err,
			Status:  http.StatusInternalServerError,
			Message: "failed to get bus stops from atb",
		}
	}
	jsonBlob, err := marshal(busStops, context.Get(req, "indent").(bool))
	if err != nil {
		log.Print(err)
		return &Error{
			error:   err,
			Status:  http.StatusInternalServerError,
			Message: "failed to marshal bus stops",
		}
	}
	w.Write(jsonBlob)
	return nil
}

func (a *Api) DeparturesHandler(w http.ResponseWriter, req *http.Request) *Error {
	vars := mux.Vars(req)
	nodeId, err := strconv.Atoi(vars["nodeId"])
	if err != nil {
		log.Print(err)
		return &Error{
			error:   err,
			Status:  http.StatusBadRequest,
			Message: "missing or invalid nodeId",
		}
	}
	busStops, err := a.getBusStops()
	if err != nil {
		log.Print(err)
		return &Error{
			error:   err,
			Status:  http.StatusInternalServerError,
			Message: "could not get bus stops from atb",
		}
	}
	_, knownBusStop := busStops.nodeIds[nodeId]
	if !knownBusStop {
		msg := fmt.Sprintf("bus stop with nodeId=%d not found", nodeId)
		return &Error{
			error:   err,
			Status:  http.StatusNotFound,
			Message: msg,
		}
	}
	departures, err := a.getDepartures(nodeId)
	if err != nil {
		log.Print(err)
		return &Error{
			error:   err,
			Status:  http.StatusInternalServerError,
			Message: "could not get departures from atb",
		}
	}
	jsonBlob, err := marshal(departures, context.Get(req, "indent").(bool))
	if err != nil {
		log.Print(err)
		return &Error{
			error:   err,
			Status:  http.StatusInternalServerError,
			Message: "failed to marshal departures",
		}
	}
	w.Write(jsonBlob)
	return nil
}

func New(client atb.Client, stopsExpiration, depExpiration time.Duration) Api {
	cache := cache.New(depExpiration, 30*time.Second)
	return Api{
		Client: client,
		cache:  cache,
		expiration: expiration{
			stops:      stopsExpiration,
			departures: depExpiration,
		},
	}
}

type appHandler func(http.ResponseWriter, *http.Request) *Error

func (fn appHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if e := fn(w, r); e != nil { // e is *Error, not os.Error.
		if e.error != nil {
			log.Print(e.error)
		}
		data, err := marshal(e, true)
		if err != nil {
			// Should never happen
			panic(err)
		}
		w.WriteHeader(e.Status)
		w.Write(data)
	}
}

func requestFilter(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, indent := r.URL.Query()["pretty"]
		context.Set(r, "indent", indent)
		w.Header().Set("Content-Type", "application/json")
		next.ServeHTTP(w, r)
	})
}

func (a *Api) ListenAndServe(addr string) error {
	r := mux.NewRouter()
	r.Handle("/api/v1/busstops", appHandler(a.BusStopsHandler))
	r.Handle("/api/v1/departures/{nodeId:[0-9]+}",
		appHandler(a.DeparturesHandler))
	http.Handle("/", requestFilter(r))
	return http.ListenAndServe(addr, nil)
}
