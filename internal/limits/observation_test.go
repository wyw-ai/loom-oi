package limits

import (
	"encoding/json"
	"math"
	"testing"
)

func TestObservationLimits(t *testing.T) {
	l := Defaults()
	o := ObservationDefaults(l)
	if err := o.Validate(l); err != nil {
		t.Fatal(err)
	}
	if o.PendingChecks != 32 || o.QuerySlots != 8 || o.ProbeWorkers != 4 || o.ProbeQueue != 32 || ObservationMaxWaiters != 16 {
		t.Fatal(o)
	}
	l.NOIS = 2
	l.MaxExposures = 1
	l.PrepareMS = 100
	o = ObservationDefaults(l)
	if err := o.Validate(l); err != nil {
		t.Fatal(err)
	}
	if o.PendingChecks != 2 || o.QuerySlots != 2 || o.ProbeWorkers != 1 || o.ProbeQueue != 1 || o.CheckMS != 100 || o.QueryMS != 100 {
		t.Fatal(o)
	}
	l = Defaults()
	o = ObservationDefaults(l)
	large := l
	large.PrepareMS = math.MaxInt64
	for _, query := range []bool{false, true} {
		x := o
		if query {
			x.QueryMS = math.MaxInt64
		} else {
			x.CheckMS = math.MaxInt64
		}
		if x.Validate(large) == nil {
			t.Fatal("accepted duration overflow")
		}
	}
	b, _ := json.Marshal(o)
	var fields map[string]int64
	if err := json.Unmarshal(b, &fields); err != nil {
		t.Fatal(err)
	}
	for name := range fields {
		t.Run("zero_"+name, func(t *testing.T) {
			copyFields := map[string]int64{}
			for k, v := range fields {
				copyFields[k] = v
			}
			copyFields[name] = 0
			bad, _ := json.Marshal(copyFields)
			var x Observation
			_ = json.Unmarshal(bad, &x)
			if x.Validate(l) == nil {
				t.Fatal("accepted zero", name)
			}
		})
	}
	for name, change := range map[string]func(*Observation){
		"message":        func(x *Observation) { x.MessageMax = 4097 },
		"pending":        func(x *Observation) { x.PendingChecks = l.NOIS + 1 },
		"query":          func(x *Observation) { x.QuerySlots = l.NOIS + 1 },
		"check_deadline": func(x *Observation) { x.CheckMS = l.PrepareMS + 1 },
		"query_deadline": func(x *Observation) { x.QueryMS = l.PrepareMS + 1 },
		"write":          func(x *Observation) { x.WriteMS = 101 },
		"refresh":        func(x *Observation) { x.RefreshMS = 1001 },
		"workers":        func(x *Observation) { x.ProbeWorkers = l.MaxExposures + 1 },
		"queue":          func(x *Observation) { x.ProbeQueue = l.MaxExposures + 1 },
	} {
		t.Run(name, func(t *testing.T) {
			x := o
			change(&x)
			if x.Validate(l) == nil {
				t.Fatal("accepted unbounded config")
			}
		})
	}
}
