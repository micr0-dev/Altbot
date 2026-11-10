/*
 * Copyright (C) 2025 Micr0Byte <micr0@micr0.dev>
 * Licensed under the GNU AFFERO GENERAL PUBLIC LICENSE Version 3 (AGPLv3)
 */

package main

import "fmt"

// Carbon footprint model for a local alt-text generation.
//
// The methodology and the reference values were put together by Vincent
// Bernstein (climate strategy and digital sustainability consultant) in an
// independent review of Altbot's footprint, published under CC-BY-SA 4.0.
// See CARBON.md for the full derivation, the data sources (Boavizta, Hubblo,
// Ember, ADEME) and the assumptions used for the allocation.
//
// Two things are accounted for:
//
//   - operational emissions: the electricity drawn for the generation, scaled
//     from the GPU draw to the whole facility with the allocated non-GPU draw
//     (the rest of the server, shared with other services) and the PUE.
//   - embodied emissions: the manufacturing of the hardware, allocated by the
//     time the hardware is used (embodied carbon / lifetime seconds).

const (
	// DefaultPUE is used when the configured PUE is not set (or invalid).
	// 1.35 is the North American average for 2026. Ask your data centre for
	// the real figure, it is always better than a generic average.
	DefaultPUE = 1.35

	// DefaultNonGPUPower is the power allocated to the rest of the server
	// (CPU, RAM, SSDs, PSUs, fans, motherboard) for a generation, in watts.
	// For the Altbot host this comes out at ~3.4 W once the 1/72 share of the
	// idle baseline and the full share of the dedicated storage are applied.
	DefaultNonGPUPower = 3.4

	secondsPerHour     = 3600.0
	millisecondsPerSec = 1000.0
	milligramsPerGram  = 1000.0
	wattsPerKilowatt   = 1000.0
)

// PowerMetricsConfig configures the energy and carbon footprint reporting.
type PowerMetricsConfig struct {
	Enabled  bool    `toml:"enabled"`
	GPUWatts float64 `toml:"gpu_watts"`

	// PUE scales the power drawn by the hardware to the power drawn by the
	// whole facility (cooling, lighting, losses...).
	PUE float64 `toml:"pue"`

	// NonGPUPower is the allocated power draw of the rest of the server, in
	// watts. It is added to the GPU draw before the PUE is applied.
	NonGPUPower float64 `toml:"non_gpu_watts"`

	// GridCarbonIntensity is the carbon intensity of the electricity mix, in
	// gCO2e per kWh. Set it to the mix of the grid the server is plugged into
	// (state or country level, not the contractual supplier). A value of 0
	// disables the carbon footprint display.
	GridCarbonIntensity float64 `toml:"grid_carbon_intensity"`

	// EmbodiedCarbonPerSecond is the manufacturing footprint allocated to one
	// second of generation, in gCO2e per second. It is the sum of the embodied
	// carbon of the allocated hardware divided by its lifetime in seconds.
	EmbodiedCarbonPerSecond float64 `toml:"embodied_carbon_gco2e_per_second"`

	ShowComparison     bool    `toml:"show_comparison"`
	CloudKWhPerRequest float64 `toml:"cloud_kwh_per_request"`
}

// Footprint is the result of the footprint calculation for one generation.
type Footprint struct {
	// EnergyWh is the electricity drawn at the facility level: GPU draw plus
	// the allocated non-GPU draw, with the PUE applied.
	EnergyWh float64

	// OperationalCO2eG is the CO2e emitted to produce EnergyWh.
	OperationalCO2eG float64

	// EmbodiedCO2eG is the CO2e emitted to manufacture the allocated hardware,
	// amortised over the duration of the generation.
	EmbodiedCO2eG float64
}

// TotalCO2eG returns the total carbon footprint of the generation in grams.
func (f Footprint) TotalCO2eG() float64 {
	return f.OperationalCO2eG + f.EmbodiedCO2eG
}

// effectivePUE returns the configured PUE, falling back to DefaultPUE when it
// is missing or not a physically meaningful value.
func (pm PowerMetricsConfig) effectivePUE() float64 {
	if pm.PUE < 1.0 {
		return DefaultPUE
	}
	return pm.PUE
}

// effectiveNonGPUPower returns the configured allocated non-GPU draw, falling
// back to DefaultNonGPUPower when it is negative.
func (pm PowerMetricsConfig) effectiveNonGPUPower() float64 {
	if pm.NonGPUPower < 0 {
		return DefaultNonGPUPower
	}
	return pm.NonGPUPower
}

// carbonEnabled reports whether a carbon footprint can be displayed. It
// requires a grid carbon intensity: without it the operational emissions are
// unknown and the total would be meaningless, so we show nothing rather than a
// number we cannot justify.
func (pm PowerMetricsConfig) carbonEnabled() bool {
	return pm.Enabled && pm.GridCarbonIntensity > 0
}

// facilityWatts returns the facility-level power attributable to a generation,
// in watts: (GPU draw + allocated non-GPU draw) * PUE.
func (pm PowerMetricsConfig) facilityWatts() float64 {
	return (pm.GPUWatts + pm.effectiveNonGPUPower()) * pm.effectivePUE()
}

// calculateFootprint returns the energy and carbon footprint of a generation
// that kept the hardware busy for processingTimeMs milliseconds.
func calculateFootprint(processingTimeMs int64, pm PowerMetricsConfig) Footprint {
	if processingTimeMs <= 0 {
		return Footprint{}
	}

	hours := float64(processingTimeMs) / millisecondsPerSec / secondsPerHour
	seconds := float64(processingTimeMs) / millisecondsPerSec

	energyWh := pm.facilityWatts() * hours

	return Footprint{
		EnergyWh:         energyWh,
		OperationalCO2eG: energyWh / wattsPerKilowatt * pm.GridCarbonIntensity,
		EmbodiedCO2eG:    pm.EmbodiedCarbonPerSecond * seconds,
	}
}

// calculatePowerConsumption keeps the historical behaviour: it returns the
// electricity drawn by the GPU alone, in Wh, without the PUE and without the
// rest of the server. It is retained so that the metrics file keeps the values
// it always had; use calculateFootprint for the real footprint.
func calculatePowerConsumption(processingTimeMs int64, gpuWatts float64) float64 {
	// Formula: Wh = (watts × processing_time_ms) ÷ (1000 × 3600)
	return (gpuWatts * float64(processingTimeMs)) / (millisecondsPerSec * secondsPerHour)
}

// getFootprintMessage returns the message appended to a reply, describing the
// energy and the carbon footprint of the generation. It returns an empty string
// when there is nothing meaningful to display.
func getFootprintMessage(lang string, processingTimeMs int64) string {
	if !config.PowerMetrics.Enabled || processingTimeMs <= 0 {
		return ""
	}

	footprint := calculateFootprint(processingTimeMs, config.PowerMetrics)

	energyTemplate := getLocalizedString(lang, "energyUsageMessage", "response")
	if energyTemplate == "" {
		energyTemplate = "\U0001F331 Energy used: %.3f Wh"
	}

	message := "\n\n" + fmt.Sprintf(energyTemplate, footprint.EnergyWh)

	if !config.PowerMetrics.carbonEnabled() {
		return message
	}

	carbonTemplate := getLocalizedString(lang, "carbonFootprintMessage", "response")
	if carbonTemplate == "" {
		carbonTemplate = "\U0001F331 Carbon footprint: %s"
	}

	return message + "\n" + fmt.Sprintf(carbonTemplate, formatCarbonFootprint(footprint.TotalCO2eG()))
}

// formatCarbonFootprint renders a carbon footprint value with a unit that keeps
// the number readable: milligrams below one gram, grams above.
func formatCarbonFootprint(co2eGrams float64) string {
	if co2eGrams < 1.0 {
		return fmt.Sprintf("%.0f mgCO2e", co2eGrams*milligramsPerGram)
	}
	return fmt.Sprintf("%.2f gCO2e", co2eGrams)
}
