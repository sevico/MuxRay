# AAC diagnostics v1

This document defines the stable diagnostic codes emitted by the bounded
AudioSpecificConfig parser and the FLV AAC tag adapter. `code` is the machine
compatibility key; consumers must not branch on the human-readable `message`.

## Syntax references

- [Adobe Flash Video File Format Specification v10.1](https://veovera.org/docs/legacy/video-file-format-v10-1-spec.pdf) defines the FLV AudioTagHeader and AACPacketType framing.
- [ISO/IEC 14496-3:2009/Amd.1:2009 sample](https://cdn.standards.iteh.ai/samples/53944/df43eb467f3f4c9e935c26babad9de3d/ISO-IEC-14496-3-2009-Amd-1-2009.pdf) contains the AudioSpecificConfig/GetAudioObjectType syntax used by this bounded subset.

## FLV AAC framing

| Code | Severity | Meaning |
|---|---|---|
| `AAC_FLV_AUDIO_HEADER_TRUNCATED` | error | The complete FLV tag has an empty payload, so its one-byte AudioTagHeader is absent. |
| `AAC_FLV_PACKET_TYPE_TRUNCATED` | error | The AudioTagHeader identifies AAC, but the following one-byte AACPacketType is absent. |
| `AAC_FLV_PACKET_TYPE_UNKNOWN` | error | AACPacketType is neither sequence header (`0`) nor raw AAC (`1`). |
| `AAC_FLV_SOUND_RATE_UNEXPECTED` | warning | The FLV AAC SoundRate field is not the canonical value `3`; the actual sample rate must still come from AudioSpecificConfig. |
| `AAC_FLV_SOUND_TYPE_UNEXPECTED` | warning | The FLV AAC SoundType field is not the canonical value `1`; the actual channel configuration must still come from AudioSpecificConfig. |

## AudioSpecificConfig

| Code | Severity | Meaning |
|---|---|---|
| `AAC_AUDIO_OBJECT_TYPE_TRUNCATED` | error | Fewer than five bits remain for an initial or core audioObjectType. |
| `AAC_AUDIO_OBJECT_TYPE_ESCAPE_TRUNCATED` | error | audioObjectType is the escape value `31`, but its six-bit suffix is truncated. |
| `AAC_AUDIO_OBJECT_TYPE_RESERVED` | error | The decoded audioObjectType is null, reserved, or outside the standardized non-reserved range. |
| `AAC_AUDIO_OBJECT_TYPE_UNSUPPORTED` | error | The object type is known, but its required object-type-specific configuration is outside the v1 parser's supported GA subset. |
| `AAC_SAMPLING_FREQUENCY_INDEX_TRUNCATED` | error | Fewer than four bits remain for samplingFrequencyIndex. |
| `AAC_SAMPLING_FREQUENCY_INDEX_RESERVED` | error | samplingFrequencyIndex is the reserved value `13` or `14`. |
| `AAC_SAMPLING_FREQUENCY_TRUNCATED` | error | samplingFrequencyIndex is `15`, but the explicit 24-bit frequency is truncated. |
| `AAC_SAMPLING_FREQUENCY_ZERO` | error | An explicit 24-bit sampling frequency is zero. |
| `AAC_CHANNEL_CONFIGURATION_TRUNCATED` | error | Fewer than four bits remain for channelConfiguration. |
| `AAC_CHANNEL_CONFIGURATION_RESERVED` | error | channelConfiguration is in the reserved range `8` through `15`. |
| `AAC_PROGRAM_CONFIG_ELEMENT_UNSUPPORTED` | error | channelConfiguration is zero and would require an unsupported program_config_element. |
| `AAC_FRAME_LENGTH_FLAG_TRUNCATED` | error | GASpecificConfig is truncated before frameLengthFlag is complete. |
| `AAC_DEPENDS_ON_CORE_CODER_TRUNCATED` | error | GASpecificConfig is truncated before dependsOnCoreCoder is complete. |
| `AAC_CORE_CODER_DELAY_TRUNCATED` | error | dependsOnCoreCoder is set, but the 14-bit coreCoderDelay is truncated. |
| `AAC_EXTENSION_FLAG_TRUNCATED` | error | GASpecificConfig is truncated before extensionFlag is complete. |
| `AAC_LAYER_NR_TRUNCATED` | error | AAC Scalable configuration is truncated before its three-bit layerNr is complete. |
| `AAC_NUM_OF_SUB_FRAME_TRUNCATED` | error | ER BSAC extension data is truncated before numOfSubFrame is complete. |
| `AAC_LAYER_LENGTH_TRUNCATED` | error | ER BSAC extension data is truncated before the 11-bit layer_length is complete. |
| `AAC_SECTION_DATA_RESILIENCE_FLAG_TRUNCATED` | error | An error-resilient extension is truncated before aacSectionDataResilienceFlag is complete. |
| `AAC_SCALEFACTOR_DATA_RESILIENCE_FLAG_TRUNCATED` | error | An error-resilient extension is truncated before aacScalefactorDataResilienceFlag is complete. |
| `AAC_SPECTRAL_DATA_RESILIENCE_FLAG_TRUNCATED` | error | An error-resilient extension is truncated before aacSpectralDataResilienceFlag is complete. |
| `AAC_EXTENSION_FLAG3_TRUNCATED` | error | An enabled GA extension is truncated before extensionFlag3 is complete. |
| `AAC_EXTENSION_FLAG3_UNSUPPORTED` | error | extensionFlag3 is set, but the additional syntax selected by that flag is outside the v1 parser scope. |
| `AAC_EP_CONFIG_TRUNCATED` | error | An error-resilient audio object type is truncated before the two-bit epConfig is complete. |
| `AAC_ERROR_PROTECTION_CONFIG_UNSUPPORTED` | error | epConfig is `2` or `3`, which requires an unsupported ErrorProtectionSpecificConfig. |
| `AAC_DIAGNOSTIC_LIMIT_REACHED` | error | The configured diagnostic budget was exhausted and further diagnostics were omitted. |

## Offsets and recovery

- `offset` and `bitOffset` identify the first unavailable or invalid source bit
  using absolute coordinates in the original FLV file.
- A warning has no `recoveryOffset` and does not make an otherwise complete AAC
  detail incomplete.
- Every error in an emitted `flv-aac-tag-detail` document has a
  `recoveryOffset`. FLV framing errors recover at the exclusive end of the
  selected Tag payload.
- When `ParseAudioSpecificConfig` is used directly, a fatal syntax diagnostic
  recovers at the exclusive end of its supplied AudioSpecificConfig span. When
  it is used through the FLV adapter, the adapter rewrites error recovery to the
  exclusive end of the complete FLV AAC payload. This guarantees that malformed
  AAC syntax cannot consume the following Tag.
- `bitsConsumed` records syntax actually consumed or inspected; recovery does
  not pretend that malformed trailing bits were successfully parsed.
- `AAC_DIAGNOSTIC_LIMIT_REACHED` preserves the retained fatal condition and its
  recovery position even if preceding FLV warnings consumed the budget.

Selection failures (`nil` source, non-Audio Tag, filtered Tag, physically
incomplete Tag, or non-AAC SoundFormat), invalid options, and `ReaderAt` failures
are Go errors rather than syntax diagnostics. A physically complete Tag with
malformed AAC remains present in the outer FLV index; only its on-demand detail
is incomplete.
