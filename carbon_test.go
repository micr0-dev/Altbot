/*
 * Copyright (C) 2025 Micr0Byte <micr0@micr0.dev>
 * Licensed under the GNU AFFERO GENERAL PUBLIC LICENSE Version 3 (AGPLv3)
 */

package main

import (
	"math"
	"testing"
)

// Values from the reference model (see CARBON.md): 200 W GPU, 3.4 W allocated
// non-GPU draw, PUE 1.35, 247 gCO2e/kWh, ~29 mgCO2e/s of embodied carbon.
func referenceConfig() PowerMetricsConfig {
	return PowerMetricsConfig{
		Enabled:                 true,
		GPUWatts:                200,
		NonGPUPower:             3.4,
		PUE:                     1.35,
		GridCarbonIntensity:     247,
		EmbodiedCarbonPerSecond: 0.0290,
	}
}

func TestFacilityWatts(t *testing.T) {
	pm := referenceConfig()

	// (200 + 3.4) * 1.35 = 274.59 W, i.e. ~275 W vs the 200 W of the GPU alone.
	want := 274.59
	if got := pm.facilityWatts(); math.Abs(got-want) > 0.01 {
		t.Errorf("facilityWatts() = %.2f, want %.2f", got, want)
	}
}

func TestCalculateFootprint(t *testing.T) {
	pm := referenceConfig()

	// 30 s of generation.
	footprint := calculateFootprint(30000, pm)

	// 274.59 W * 30 s = 8237.7 W·s = 2.288 Wh
	wantEnergyWh := 274.59 * 30 / 3600
	if math.Abs(footprint.EnergyWh-wantEnergyWh) > 1e-9 {
		t.Errorf("EnergyWh = %.6f, want %.6f", footprint.EnergyWh, wantEnergyWh)
	}

	// 2.288 Wh / 1000 * 247 gCO2e/kWh = 0.565 gCO2e
	wantOperational := wantEnergyWh / 1000 * 247
	if math.Abs(footprint.OperationalCO2eG-wantOperational) > 1e-9 {
		t.Errorf("OperationalCO2eG = %.6f, want %.6f", footprint.OperationalCO2eG, wantOperational)
	}

	// 0.0290 gCO2e/s * 30 s = 0.87 gCO2e
	wantEmbodied := 0.0290 * 30
	if math.Abs(footprint.EmbodiedCO2eG-wantEmbodied) > 1e-9 {
		t.Errorf("EmbodiedCO2eG = %.6f, want %.6f", footprint.EmbodiedCO2eG, wantEmbodied)
	}
}

func TestFootprintPerSecond(t *testing.T) {
	pm := referenceConfig()

	// One second of generation.
	footprint := calculateFootprint(1000, pm)

	// 274.59 W / 3600 * 247 / 1000 = 18.8 mgCO2e operational
	// + 29.0 mgCO2e embodied = ~47.9 mgCO2e
	want := 0.04786
	if got := footprint.TotalCO2eG(); math.Abs(got-want) > 0.0001 {
		t.Errorf("TotalCO2eG() per second = %.5f g, want %.5f g", got, want)
	}
}

func TestCalculateFootprintZeroDuration(t *testing.T) {
	footprint := calculateFootprint(0, referenceConfig())

	if footprint.EnergyWh != 0 || footprint.TotalCO2eG() != 0 {
		t.Errorf("expected an empty footprint for a zero duration, got %+v", footprint)
	}
}

func TestPUEFallback(t *testing.T) {
	if got := (PowerMetricsConfig{PUE: 0}).effectivePUE(); got != DefaultPUE {
		t.Errorf("effectivePUE() = %.2f, want the default %.2f", got, DefaultPUE)
	}
	if got := (PowerMetricsConfig{PUE: 0.8}).effectivePUE(); got != DefaultPUE {
		t.Errorf("effectivePUE() = %.2f for a PUE below 1, want the default %.2f", got, DefaultPUE)
	}
	if got := (PowerMetricsConfig{PUE: 1.2}).effectivePUE(); got != 1.2 {
		t.Errorf("effectivePUE() = %.2f, want 1.20", got)
	}
}

func TestNonGPUPowerFallback(t *testing.T) {
	if got := (PowerMetricsConfig{NonGPUPower: -1}).effectiveNonGPUPower(); got != DefaultNonGPUPower {
		t.Errorf("effectiveNonGPUPower() = %.2f, want the default %.2f", got, DefaultNonGPUPower)
	}
	if got := (PowerMetricsConfig{}).effectiveNonGPUPower(); got != 0 {
		t.Errorf("effectiveNonGPUPower() = %.2f for an explicit 0, want 0", got)
	}
}

func TestCarbonEnabled(t *testing.T) {
	pm := referenceConfig()
	if !pm.carbonEnabled() {
		t.Error("carbon footprint should be enabled with a grid intensity")
	}

	pm.GridCarbonIntensity = 0
	if pm.carbonEnabled() {
		t.Error("carbon footprint should be disabled without a grid intensity")
	}

	pm = referenceConfig()
	pm.Enabled = false
	if pm.carbonEnabled() {
		t.Error("carbon footprint should be disabled when power metrics are off")
	}
}

func TestFormatCarbonFootprint(t *testing.T) {
	cases := []struct {
		grams float64
		want  string
	}{
		{0.0478, "48 mgCO2e"},
		{0.999, "999 mgCO2e"},
		{1.0, "1.00 gCO2e"},
		{12.345, "12.35 gCO2e"},
	}

	for _, c := range cases {
		if got := formatCarbonFootprint(c.grams); got != c.want {
			t.Errorf("formatCarbonFootprint(%.4f) = %q, want %q", c.grams, got, c.want)
		}
	}
}

func TestCalculatePowerConsumptionUnchanged(t *testing.T) {
	// The historical helper keeps returning the GPU draw alone, without the
	// PUE and without the rest of the server.
	got := calculatePowerConsumption(30000, 200)
	want := 200.0 * 30000 / 3600000.0

	if math.Abs(got-want) > 1e-12 {
		t.Errorf("calculatePowerConsumption() = %.9f, want %.9f", got, want)
	}
}
