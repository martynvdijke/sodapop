package checkins_test

import (
	"testing"
	"time"

	"sodapop/internal/checkins"
)

func TestInsightsSpendByCurrency(t *testing.T) {
	e := newTestEnv(t)

	create := func(price *int, currency string) {
		t.Helper()
		if _, _, err := e.svc.Create(e.ctx, e.alice, checkins.CreateInput{
			DrinkID: e.drink.ID, PriceCents: price, PriceCurrency: currency,
		}); err != nil {
			t.Fatalf("create: %v", err)
		}
	}
	create(intPtr(250), "EUR")
	create(intPtr(350), "EUR")
	create(intPtr(100), "USD")
	create(nil, "")

	ins, err := e.svc.Insights(e.ctx, e.alice.ID, checkins.InsightsOptions{Days: 30, Now: time.Now()})
	if err != nil {
		t.Fatalf("insights: %v", err)
	}
	if ins.Pours != 4 {
		t.Fatalf("pours = %d", ins.Pours)
	}
	want := []checkins.CurrencySpend{
		{Currency: "EUR", TotalCents: 600, Count: 2},
		{Currency: "USD", TotalCents: 100, Count: 1},
	}
	if len(ins.Spend) != len(want) {
		t.Fatalf("spend = %+v, want %+v", ins.Spend, want)
	}
	for i := range want {
		if ins.Spend[i] != want[i] {
			t.Fatalf("spend[%d] = %+v, want %+v", i, ins.Spend[i], want[i])
		}
	}
}

func TestInsightsSpendEmptyWithoutPrices(t *testing.T) {
	e := newTestEnv(t)
	e.createCheckin(t, e.alice, "public")

	ins, err := e.svc.Insights(e.ctx, e.alice.ID, checkins.InsightsOptions{Days: 7, Now: time.Now()})
	if err != nil {
		t.Fatalf("insights: %v", err)
	}
	if len(ins.Spend) != 0 {
		t.Fatalf("spend = %+v, want empty", ins.Spend)
	}
}
