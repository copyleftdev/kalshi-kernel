package marketdata

import (
	"context"
	"net/http"
	"testing"
)

func i64(v int64) *int64 { return &v }
func b(v bool) *bool     { return &v }

func TestGetWeatherIndexProjectsTimeseries(t *testing.T) {
	client := fixtureServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/trade-api/v2/live_data/weather/miami" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("last_sec"); got != "3600" {
			t.Errorf("last_sec = %q", got)
		}
		if got := r.URL.Query().Get("detailed"); got != "true" {
			t.Errorf("detailed = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"city":"miami","config_version":"miami-temperature-v1.0","units":"fahrenheit",
			"timeseries":[
				{"t":1787600000000,"v":91.37,"status":"normal","contributors":5,
				 "stations":[{"station_id":"KMIA1M","code":"ok","source":"hf_asos","temp_f":91.4}]},
				{"t":1787600060000,"status":"incomplete",
				 "stations":[{"station_id":"KMIA1M","code":"pending","source":"hf_asos","temp_f":91.2}]},
				{"t":1787600120000,"v":91.44,"status":"degraded","contributors":4,
				 "stations":[{"station_id":"KMIA1M","code":"ok","source":"metar","temp_f":91.4},
				            {"station_id":"KOPF1M","code":"missing"}]}
			]}`))
	})
	idx, err := client.GetWeatherIndex(context.Background(), "miami", nil, nil, i64(3600), true)
	if err != nil {
		t.Fatalf("GetWeatherIndex: %v", err)
	}
	if idx.City != "miami" || idx.Units != "fahrenheit" || idx.ConfigVersion != "miami-temperature-v1.0" {
		t.Fatalf("header wrong: %+v", idx)
	}
	if len(idx.Timeseries) != 3 {
		t.Fatalf("want 3 points, got %d", len(idx.Timeseries))
	}
	p0 := idx.Timeseries[0]
	if p0.V == nil || *p0.V != 91.37 || p0.Status != "normal" || p0.Contributors == nil || *p0.Contributors != 5 {
		t.Fatalf("point 0 wrong: %+v", p0)
	}
	// incomplete point: value and contributors must stay nil (real gap)
	p1 := idx.Timeseries[1]
	if p1.V != nil || p1.Contributors != nil || p1.Status != "incomplete" {
		t.Fatalf("incomplete point zero-filled: %+v", p1)
	}
	if len(p1.Stations) != 1 || p1.Stations[0].Code != "pending" {
		t.Fatalf("pending station wrong: %+v", p1.Stations)
	}
	// degraded point with a missing station: temp_f nil, no source
	p2 := idx.Timeseries[2]
	if p2.Status != "degraded" || len(p2.Stations) != 2 || p2.Stations[1].TempF != nil || p2.Stations[1].Source != nil {
		t.Fatalf("degraded point wrong: %+v", p2.Stations)
	}
}

func TestGetWeatherIndexWindowParams(t *testing.T) {
	client := fixtureServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"city":"lax","units":"fahrenheit","timeseries":[]}`))
	})
	// from/to pair passes through as query params
	_, err := client.GetWeatherIndex(context.Background(), "lax", i64(1000), i64(2000), nil, false)
	if err != nil {
		t.Fatalf("from/to call: %v", err)
	}
	// last_sec + from is rejected locally
	if _, err := client.GetWeatherIndex(context.Background(), "lax", i64(1000), nil, i64(60), false); Code(err) != errBadInput {
		t.Fatalf("want errBadInput for last_sec+from, got %v", err)
	}
	// bare from rejected locally
	if _, err := client.GetWeatherIndex(context.Background(), "lax", i64(1000), nil, nil, false); Code(err) != errBadInput {
		t.Fatalf("want errBadInput for bare from, got %v", err)
	}
	// bare to rejected locally
	if _, err := client.GetWeatherIndex(context.Background(), "lax", nil, i64(1000), nil, false); Code(err) != errBadInput {
		t.Fatalf("want errBadInput for bare to, got %v", err)
	}
	// empty city rejected locally
	if _, err := client.GetWeatherIndex(context.Background(), "", nil, nil, nil, false); Code(err) != errBadInput {
		t.Fatalf("want errBadInput for empty city, got %v", err)
	}
}
