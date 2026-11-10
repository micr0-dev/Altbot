/*
 * Copyright (C) 2025 Micr0Byte <micr0@micr0.dev>
 * Licensed under the GNU AFFERO GENERAL PUBLIC LICENSE Version 3 (AGPLv3)
 */

package main

import (
	"math"
	"strings"
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

// Reference values taken from the reviewed spreadsheet (Vincent Bernstein,
// CC-BY-SA 4.0), in kgCO2e per second. If any of these tests fail, the
// implementation has drifted from the published methodology.
const (
	sheetGPU   = 2.89719660013979e-05
	sheetRAM   = 2.39551141116850e-09
	sheetCPU   = 2.27853355467623e-09
	sheetSSD   = 4.73080745344796e-09
	sheetOther = 1.47277589953839e-08
	sheetElec  = 1.88610633649479e-05
	sheetTotal = 4.78571619777605e-05

	// Power allocated to the generation, in watts: the 200 W GPU plus the
	// 3.4355 W share of the rest of the server.
	sheetAllocatedW = 203.435486111111
	// Facility draw relative to the GPU draw alone (274.6 W / 200 W).
	sheetRatio = 1.37318953125
)

// sheetConfig returns the configuration that reproduces the reviewed sheet.
func sheetConfig() PowerMetricsConfig {
	return PowerMetricsConfig{
		Enabled:                 true,
		GPUWatts:                200,
		NonGPUPower:             sheetAllocatedW - 200,
		PUE:                     1.35,
		GridCarbonIntensity:     247.234,
		EmbodiedCarbonPerSecond: (sheetGPU + sheetRAM + sheetCPU + sheetSSD + sheetOther) * 1000,
	}
}

// TestMatchesReviewedSheet checks the implementation against every published
// per-second figure: the facility draw, the operational and embodied split, the
// total, and the shares quoted in the review.
func TestMatchesReviewedSheet(t *testing.T) {
	pm := sheetConfig()

	// Facility watts: 203.435 W allocated, scaled by the PUE.
	if got, want := pm.facilityWatts(), sheetAllocatedW*1.35; math.Abs(got-want) > 1e-6 {
		t.Errorf("facilityWatts() = %.9f W, sheet says %.9f W", got, want)
	}

	// The draw is 37 % above the 200 W of the GPU alone.
	if got := pm.facilityWatts() / 200; math.Abs(got-sheetRatio) > 1e-9 {
		t.Errorf("facility draw is %.9f x the GPU draw, sheet says %.9f", got, sheetRatio)
	}

	// Exactly one second, so the values are directly comparable.
	fp := calculateFootprint(1000, pm)

	if got, want := fp.OperationalCO2eG, sheetElec*1000; math.Abs(got-want) > 1e-12 {
		t.Errorf("operational CO2e = %.12e g/s, sheet says %.12e", got, want)
	}

	embodiedTotal := (sheetGPU + sheetRAM + sheetCPU + sheetSSD + sheetOther) * 1000
	if got, want := fp.EmbodiedCO2eG, embodiedTotal; math.Abs(got-want) > 1e-12 {
		t.Errorf("embodied CO2e = %.12e g/s, sheet says %.12e", got, want)
	}

	if got, want := fp.TotalCO2eG(), sheetTotal*1000; math.Abs(got-want) > 1e-12 {
		t.Errorf("total CO2e = %.12e g/s, sheet says %.12e", got, want)
	}

	// The headline figure: about 48 mgCO2e per second.
	if got := fp.TotalCO2eG() * 1000; math.Abs(got-47.857) > 0.001 {
		t.Errorf("total = %.4f mgCO2e/s, want 47.857", got)
	}

	// GPU manufacturing is 60.5 % and electricity 39.4 % of the total.
	gpuShare := sheetGPU * 1000 / fp.TotalCO2eG() * 100
	if math.Abs(gpuShare-60.54) > 0.01 {
		t.Errorf("GPU manufacturing share = %.2f %%, want 60.54 %%", gpuShare)
	}
	elecShare := fp.OperationalCO2eG / fp.TotalCO2eG() * 100
	if math.Abs(elecShare-39.41) > 0.01 {
		t.Errorf("electricity share = %.2f %%, want 39.41 %%", elecShare)
	}

	// Everything that is not the GPU or the electricity is negligible, and the
	// three shares must account for the whole.
	restShare := 100 - gpuShare - elecShare
	if math.Abs(restShare-0.05) > 0.01 {
		t.Errorf("remaining share = %.2f %%, want 0.05 %%", restShare)
	}
}

// TestUnitsAreGramsNotMilligrams guards the unit conversion between the sheet
// (kgCO2e/s) and the config (gCO2e/s). The whole footprint is well under a
// gram per second, so a factor of 1000 in either direction is immediately
// visible here.
func TestUnitsAreGramsNotMilligrams(t *testing.T) {
	fp := calculateFootprint(1000, sheetConfig())

	// 47.86 mg/s is 0.04786 g/s. A result near 47.86 would mean the value was
	// left in mg, and one near 4.786e-5 would mean it was left in kg.
	if fp.TotalCO2eG() < 0.04 || fp.TotalCO2eG() > 0.06 {
		t.Fatalf("total = %g for one second, want ~0.0479 g; a unit conversion is wrong", fp.TotalCO2eG())
	}
}

// TestRenderedUnits checks the displayed string for a one second generation,
// which is the case where mgCO2e must be used rather than gCO2e.
func TestRenderedMessage(t *testing.T) {
	if err := loadLocalizations(); err != nil {
		t.Fatalf("loading localizations: %v", err)
	}

	config.Localization.DefaultLanguage = "en"
	config.PowerMetrics = sheetConfig()

	message := getFootprintMessage("en", 1000)
	if !strings.Contains(message, "48 mgCO2e") {
		t.Errorf("one second should render as 48 mgCO2e, got %q", message)
	}
	if !strings.Contains(message, "Energy used") {
		t.Errorf("energy line missing from %q", message)
	}

	// Above a gram the unit switches.
	message = getFootprintMessage("en", 30000)
	if !strings.Contains(message, "1.44 gCO2e") {
		t.Errorf("thirty seconds should render as 1.44 gCO2e, got %q", message)
	}

	// Without a grid intensity the carbon line is absent, not zero.
	pm := sheetConfig()
	pm.GridCarbonIntensity = 0
	config.PowerMetrics = pm
	if message := getFootprintMessage("en", 30000); strings.Contains(message, "Carbon") {
		t.Errorf("carbon line should be hidden without a grid intensity, got %q", message)
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
