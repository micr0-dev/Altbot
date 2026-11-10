# Carbon footprint of an alt-text generation

Altbot reports, alongside each generated alt-text, how much electricity the
generation used and how much CO2e it emitted.

The second figure comes from an independent review of Altbot's footprint. The
methodology and the reference values below are the work of **Vincent Bernstein**
(climate strategy and digital sustainability consultant), who reviewed Altbot's
energy reporting and extended it to a full carbon footprint. His calculations
are published under
[CC-BY-SA 4.0](https://creativecommons.org/licenses/by-sa/4.0/).

This document explains what the numbers mean, where the data comes from, and
how to parameterise them for your own instance.

## What is counted

The footprint has two parts.

**Operational emissions** are the emissions of the electricity consumed while
the hardware runs. The electricity number Altbot historically displayed only
covered the GPU, so it missed two things:

- the rest of the server (CPU, RAM, storage, PSUs, fans, motherboard), and
- the overhead of the data centre itself, expressed as the PUE (Power Usage
  Effectiveness): for every watt drawn by the server, the facility draws
  `PUE - 1` extra watts for cooling, lighting, power distribution losses, and so
  on.

**Embodied emissions** are the emissions of manufacturing the hardware, spread
over the time that hardware is in service. A GPU does not emit only when it
computes: building it emitted carbon, and the only honest way to attribute it is
to divide it by the lifetime of the part, then by the share of that part the
workload uses.

## The formula

For a generation that kept the hardware busy for `t` milliseconds:

```
P_facility = (P_gpu + P_other) × PUE                      [W]
E          = P_facility × t / 3 600 000                   [Wh]
CO2e_oper  = E / 1000 × I_grid                            [gCO2e]
CO2e_emb   = C_embodied_per_second × t / 1000             [gCO2e]
CO2e_total = CO2e_oper + CO2e_emb                         [gCO2e]
```

where:

| Symbol | Meaning | Config key |
| --- | --- | --- |
| `P_gpu` | GPU draw during generation | `gpu_watts` |
| `P_other` | Allocated draw of the rest of the server | `non_gpu_watts` |
| `PUE` | Power Usage Effectiveness of the data centre | `pue` |
| `I_grid` | Carbon intensity of the grid, in gCO2e/kWh | `grid_carbon_intensity` |
| `C_embodied_per_second` | Manufacturing footprint amortised over usage | `embodied_carbon_gco2e_per_second` |

`E` is what Altbot displays as `Energy used`. Note that it is now a facility-level
figure, PUE included, so it is roughly 1.35× larger than the GPU-only figure
displayed by earlier versions. If you compare against old logs, that is why.

## The reference values

The review produced these figures for the Altbot host: a Dell PowerEdge R740xd
with 2× Intel Xeon Gold 6140, 128 GB of RAM, an NVIDIA RTX A5500, 2× 960 GB SAS
SSD, 2× 120 GB SATA SSD, 6× 2 TB SAS HDD, and 2× 750 W PSUs, colocated in
Illinois, United States.

**Power**

| Item | Value | Source |
| --- | --- | --- |
| GPU draw during generation | 200 W | measured |
| Allocated rest of the server | 3.4355 W | derived, see below |
| PUE | 1.35 | North American average, 2026 |
| **Facility draw** | **274.6 W** | (200 + 3.4355) × 1.35 |

That is 37 % more than the 200 W of the GPU alone, which is what previous
versions of Altbot reported.

> **One inconsistency in the reviewed calculation.** The sheet's own power cell
> (`B37`) computes 203.435486111111 W, which is 3.4355 W on top of the GPU, and
> the 275 W and the 37 % above both follow from that figure. But the formula in
> that cell does not produce it. Read literally it is
> `(4/128×3 + 0.055×4) + (2/72×36×0.75×140/72) + (4/2160×2160×8) + 230`, i.e.
> `0.31375 + 1.45833 + 32 + 230 = 263.77 W`, which would put the facility draw at
> 356 W and the increase at 78 %. Most of the gap is the SSD term: the
> `×4/2160` and `×2160` cancel, so it charges 8 W per drive for four drives
> rather than the ~1 W of an active SAS SSD. The `+230` term is a placeholder
> for the rest of the server, and the value it holds (230 W) also contradicts
> the "3.4 W" description.
>
> The totals documented here use the **result** of that cell, because that is
> what was reviewed, quoted, and published. The formula should be corrected in
> the source spreadsheet; if it is, the numbers above will need revisiting.

The 3.4355 W of non-GPU draw comes from allocating the server's own consumption the
way the hardware is shared. The GPU is dedicated to Altbot, so the whole GPU
draw is charged to it. Everything else is allocated by usage: 1/72 of the idle
baseline (1 vCPU out of 72), the full share of the SSD mirror that is dedicated
to the bot, and 1/72 of the shared boot SSDs and HDDs. For the Altbot host this
comes out at 3.4355 W. On a busier or larger machine, or one where the bot has less
of the hardware to itself, it will be higher.

**Electricity**

| Item | Value | Source |
| --- | --- | --- |
| Server location | Illinois, United States | |
| Grid carbon intensity | 247 gCO2e/kWh | [Ember, 2025](https://ember-energy.org/data/us-electricity-data/) |

The grid mix of the territory the server is plugged into is used, not the
contractual supply of the operator. This is the approach recommended by the
French environmental agency (ADEME): the electrons that actually reach the data
centre are the ones produced by the local mix.

**Hardware**

Embodied carbon figures come from
[Boavizta](https://doc.api.boavizta.org/Explanations/components/component/) for
RAM, CPU, SSD, and other server equipment, and from
[Hubblo](https://data.hubblo.org/gpu) for the GPU.

| Component | Manufacturing footprint | Lifetime | Allocated | mgCO2e/s |
| --- | --- | --- | --- | --- |
| GPU (A5500, 24 GB) | 144 kgCO2e | 9 years at 1.75 % duty cycle | 100 % | 28.97 |
| RAM (4× 32 GB) | 0.83 kgCO2e | 11 years | 0.5 GB | 0.0024 |
| CPU (2× Xeon Gold 6140) | 0.79 kgCO2e | 11 years | 1 vCPU of 72 | 0.0023 |
| SSD (4 drives, 2 160 GB) | 1.64 kgCO2e | 11 years | 30 GB | 0.0047 |
| Other server equipment | 368.1 kgCO2e | 11 years | 1/72 | 0.0147 |

The GPU lifetime is not a wall-clock lifetime: at a 1.75 % duty cycle, a GPU
rated for 9 years of operation lasts far longer in calendar time. The allocation
above uses 9 years of *active* time, which is the conservative reading.

**Total**

| Contribution | mgCO2e/s | Share |
| --- | --- | --- |
| GPU manufacturing | 28.97 | 60.5 % |
| Electricity | 18.86 | 39.4 % |
| RAM, CPU, SSD, other | 0.024 | 0.05 % |
| **Total** | **47.86** | **100 %** |

So roughly **48 mgCO2e per second** of GPU time, of which **29 mg** is hardware
and **19 mg** is electricity. Everything that is not the GPU or the electricity
is negligible for this allocation.

Note the unit: 48 mgCO2e is **0.048 gCO2e**, forty-eight thousandths of a gram of
CO2e. It is written in milligrams because the number is much easier to read that
way, not because it is a different quantity. `carbon_test.go`
(`TestMatchesReviewedSheet`, `TestUnitsAreGramsNotMilligrams`) pins every
per-second figure above against the reviewed spreadsheet, including that
conversion.

Which yields, for the configuration above:

```toml
[power_metrics]
enabled = true
gpu_watts = 200
non_gpu_watts = 3.4355
pue = 1.35
grid_carbon_intensity = 247
embodied_carbon_gco2e_per_second = 0.0290
```

A 30-second generation then reports about 2.29 Wh and 1.44 gCO2e.

## Parameterising your own instance

The defaults shipped in `example.config.toml` deliberately leave the carbon
values at `0`: they are specific to a machine, a grid, and an allocation, and
shipping someone else's numbers would be worse than shipping none.

To turn the carbon footprint on:

1. **`grid_carbon_intensity`** — look up the carbon intensity of the grid where
   the server physically is, in gCO2e/kWh. State-level for the US (eGRID),
   country-level elsewhere. Do not use the figure from a renewable energy
   contract: the local mix is what the data centre actually draws on.
2. **`pue`** — ask your data centre or hosting provider. If they do not know,
   the North American average is around 1.35, European averages are lower
   (around 1.2). A PUE below 1 is not physical and is ignored.
3. **`non_gpu_watts`** — the share of the rest of the machine that belongs to the
   bot. If the bot owns the whole machine, measure the server draw and subtract
   the GPU draw. If it shares the machine, allocate by the fraction of a shared
   resource the bot uses, as done above.
4. **`embodied_carbon_gco2e_per_second`** — the manufacturing footprint of the
   allocated hardware, in gCO2e, divided by its lifetime in seconds. Use
   Boavizta or Hubblo for the per-component figures.

Leaving `grid_carbon_intensity` at `0` disables the carbon line but keeps the
energy line; Altbot will not display a carbon total it cannot justify.

## Limitations

- The numbers are estimates built on generic component figures, not on a
  teardown of the specific parts. Expect them to be right to an order of
  magnitude, not to three significant digits.
- The PUE is an average for a class of data centres. Real PUE varies with the
  season, the load, and the cooling design.
- Grid intensity varies by the hour. A yearly average is used, not the
  marginal emissions of the moment the request was served.
- The embodied carbon of the network, of the client device, and of the training
  of the model are out of scope. So is the storage of the generated text.
- The allocation assumes the GPU is used by the bot and by nothing else. If it
  is shared, the GPU share should be reduced accordingly.

## Credits

Methodology, reference values, and the original spreadsheet: **Vincent
Bernstein** ([vincent.bernstein.me](https://vincent.bernstein.me),
[@vincent@floss.social](https://floss.social/@vincent)), licensed
[CC-BY-SA 4.0](https://creativecommons.org/licenses/by-sa/4.0/).

Component data: [Boavizta](https://boavizta.org),
[Hubblo](https://hubblo.org). Grid data:
[Ember](https://ember-energy.org). Methodological reference: ADEME.