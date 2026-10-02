We now know the DXGI side is working and the remaining failure is Media Foundation encoder initialization:

IMFTransform::SetOutputType(H264) failed: 0x80070057 (E_INVALIDARG)

Stop doing one-HRESULT-at-a-time patching.

Perform a complete audit of internal/remote/encoder/mft_windows.go against the actual Windows SDK/API documentation and fix the encoder implementation as a coherent unit.

Requirements:

1. Get the real Windows Media Foundation headers available locally:
   mftransform.h
   mfapi.h
   mfidl.h
   wmcodecdsp.h
   and any related headers required.

2. Verify mechanically, not from memory:
   - CLSID_CMSH264EncoderMFT
   - IID_IMFTransform
   - every IMFTransform vtable index used
   - every IMFMediaType/attribute interface vtable index used
   - every MF_* attribute GUID
   - every COM method signature
   - every HRESULT/argument type

3. Specifically verify the complete IMFTransform initialization sequence against Microsoft's H.264 encoder documentation.
   Do not assume the current SetOutputType-first approach is correct.
   Confirm the required order of:
   - encoder property/ICodecAPI configuration
   - SetOutputType
   - input-type discovery/selection
   - SetInputType
   - streaming initialization

4. Verify the output media type contains the required H.264 attributes:
   MF_MT_MAJOR_TYPE = MFMediaType_Video
   MF_MT_SUBTYPE = MFVideoFormat_H264
   MF_MT_AVG_BITRATE
   MF_MT_FRAME_RATE
   MF_MT_FRAME_SIZE
   MF_MT_INTERLACE_MODE
   MF_MT_MPEG2_PROFILE

   Keep MF_MT_MPEG2_LEVEL optional unless there is a documented reason to set it.

5. Do not blindly hardcode a profile value.
   Verify the exact Windows SDK constant/value for eAVEncH264VProfile_Base and use the documented value.

6. Prefer querying the MFT for its available input/output types rather than assuming a hand-constructed type is accepted.
   Use GetOutputAvailableType / GetInputAvailableType where appropriate and validate the actual returned media type before setting it.

7. Audit ALL manual COM vtable calls in the encoder, not only SetOutputType.
   This is the important part: we have already discovered multiple bad indices in this project.

8. Add runtime diagnostics that enumerate the MFT's advertised input/output media types when negotiation fails:
   - subtype
   - width/height
   - frame rate
   - profile
   - HRESULT
   This should make future incompatibilities diagnosable without another blind patch.

9. Add regression tests for every hardcoded GUID/index that can reasonably be tested.
   Keep platform-specific runtime integration tests separate from pure unit tests.

10. Preserve:
   - pure Go / CGO_ENABLED=0 build
   - single Windows executable
   - current DXGI capture implementation
   - Pion WebRTC architecture
   - existing security and IPC architecture

11. Do NOT replace the encoder with FFmpeg, x264, OpenH264, mediadevices, or another external codec dependency.

12. Run:
   go test ./...
   go build -v -o bin\PC-Remote.exe .\cmd\laptopcontrol

13. Do NOT push the changes yet.

The goal is not "make this HRESULT go away".
The goal is to make the Media Foundation encoder implementation correct against the actual Windows ABI/API and independently diagnosable.

---

## Resolution Summary: Complete Media Foundation Encoder Audit

### Root Causes Discovered & Fixed
1. **Corrupted GUIDs**:
   - `guidMFMTMajorType` was `48eba18e-f827-4970-b450-cb99aa1522d7` instead of canonical `48eba18e-f8c9-4687-bf11-0a74c9f96a8f`.
   - `guidMFMTInterlaceMode` was `e272446c-e767-4972-b012-18d5226f0e39` instead of canonical `e2724bb8-e676-4806-b4b2-a8d6efb44ccd`.
   - Result: `SetOutputType(H264)` was failing with `0x80070057` (`E_INVALIDARG`) because `MF_MT_MAJOR_TYPE` and `MF_MT_INTERLACE_MODE` were never recognized by Media Foundation.
2. **`IMFSample` Vtable Off-by-One**:
   - `SetSampleTime` index was 37 instead of 36 (`37` is `GetSampleDuration`).
   - `SetSampleDuration` index was 39 instead of 38 (`39` is `GetBufferCount`).
3. **`MF_E_TRANSFORM_NEED_MORE_INPUT` Constant**:
   - Was defined as `0xC00D6D9F` instead of `0xC00D6D72`.
4. **Output Buffer Capacity**:
   - Was hardcoded to `width * height` (921,600 bytes for 720p), which caused `0xC00D36B1` (`MF_E_BUFFERTOOSMALL`).
   - Now dynamically queries `IMFTransform::GetOutputStreamInfo` (`streamInfo.cbSize` = 1,728,000 bytes).
5. **Real-time Low Latency Mode**:
   - Configured `CODECAPI_AVLowLatencyMode = VARIANT_TRUE` on `ICodecAPI`. Eliminates multi-frame buffering latency so frames output immediately starting from frame 0.
6. **Dynamic Keyframe Generation**:
   - Connected `RequestKeyFrame()` to `CODECAPI_AVEncVideoForceKeyFrame` via `ICodecAPI`.
7. **Type Negotiation & Diagnostics**:
   - Added prototype query via `GetOutputAvailableType(0, 0)`.
   - Added `GetInputAvailableType` verification for NV12 support.
   - Added `dumpMFTTypes` to log available input/output types on negotiation failure.

### Tests & Build Status
- `go test ./...` passed across all packages.
- `TestMFT_GUIDsAgainstWindowsSDK`: PASSED (verified all GUIDs against official Windows SDK headers).
- `TestMFT_VTableIndices`: PASSED (verified all vtable indices across `IMFTransform`, `ICodecAPI`, `IMFSample`, `IMFMediaBuffer`, `IMFAttributes`).
- `TestMFT_EncoderInitAndEncodeLive`: PASSED (verified end-to-end H.264 live encoding, zero dropped frames, dynamic IDR keyframe production on request).
- Built `bin\PC-Remote.exe` cleanly.