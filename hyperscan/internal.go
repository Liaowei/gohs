package hyperscan

import (
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unsafe"
)

/*
#cgo pkg-config: libhs
#cgo linux LDFLAGS: -lm -lstdc++
#cgo darwin LDFLAGS: -lstdc++

#include <stdlib.h>
#include <limits.h>
#include <stdint.h>

#include <hs.h>

static inline void* aligned64_malloc(size_t size) {
	void* result;
#ifdef _WIN32
	result = _aligned_malloc(size, 64);
#else
	if (posix_memalign(&result, 64, size)) {
		result = 0;
	}
#endif
	return result;
}

static inline void aligned64_free(void *ptr) {
#ifdef _WIN32
	_aligned_free(ptr);
#else
	free(ptr);
#endif
}

#define DEFINE_ALLOCTOR(ID, TYPE) \
	extern void *hs ## ID ## Alloc(size_t size); \
	extern void hs ## ID ## Free(void *ptr); \
	static inline void *hs ## ID ## Alloc_cgo(size_t size) { return hs ## ID ## Alloc(size); } \
	static inline void hs ## ID ## Free_cgo(void *ptr) { hs ## ID ## Free(ptr); } \
	static inline hs_error_t hs_set_ ## TYPE ## _allocator_cgo() \
	{ return hs_set_ ## TYPE ## _allocator(hs ## ID ## Alloc_cgo, hs ## ID ## Free_cgo); } \
	static inline hs_error_t hs_clear_ ## TYPE ## _allocator_cgo() \
	{ return hs_set_ ## TYPE ## _allocator(NULL, NULL); }

DEFINE_ALLOCTOR(Db, database);
DEFINE_ALLOCTOR(Misc, misc);
DEFINE_ALLOCTOR(Scratch, scratch);
DEFINE_ALLOCTOR(Stream, stream);

extern int hsMatchEventCallback(unsigned int id, unsigned long long from, unsigned long long to, unsigned int flags, void *context);

static inline
hs_error_t hs_scan_cgo(const hs_database_t *db, const char * data, unsigned int length, unsigned int flags, hs_scratch_t * scratch, uintptr_t context) {
	return hs_scan(db, data, length, flags, scratch, hsMatchEventCallback, (void *) context);
}

static inline
hs_error_t hs_scan_vector_cgo(const hs_database_t *db, const char *const *data, const unsigned int *length, unsigned int count, unsigned int flags, hs_scratch_t *scratch, uintptr_t context) {
	return hs_scan_vector(db, data, length, count, flags, scratch, hsMatchEventCallback, (void *) context);
}

static inline
hs_error_t hs_scan_stream_cgo(hs_stream_t *id, const char * data, unsigned int length, unsigned int flags, hs_scratch_t *scratch, uintptr_t context) {
	return hs_scan_stream(id, data, length, flags, scratch, hsMatchEventCallback, (void *) context);
}

static inline
hs_error_t hs_close_stream_cgo(hs_stream_t *id, hs_scratch_t *scratch, uintptr_t context) {
	return hs_close_stream(id, scratch, hsMatchEventCallback, (void *) context);
}

static inline
hs_error_t hs_reset_stream_cgo(hs_stream_t *id, unsigned int flags, hs_scratch_t *scratch, uintptr_t context) {
	return hs_reset_stream(id, flags, scratch, hsMatchEventCallback, (void *) context);
}

static inline
hs_error_t hs_reset_and_copy_stream_cgo(hs_stream_t *to_id, const hs_stream_t *from_id, hs_scratch_t *scratch, uintptr_t context) {
	return hs_reset_and_copy_stream(to_id, from_id, scratch, hsMatchEventCallback, (void *) context);
}

static inline
hs_error_t hs_reset_and_expand_stream_cgo(hs_stream_t *stream, const char *data, unsigned int length, hs_scratch_t *scratch, uintptr_t context) {
	return hs_reset_and_expand_stream(stream, data, length, scratch, hsMatchEventCallback, (void *) context);
}
*/
import "C"

// Pattern flags
type CompileFlag uint

const (
	Caseless        CompileFlag = C.HS_FLAG_CASELESS     // Set case-insensitive matching.
	DotAll          CompileFlag = C.HS_FLAG_DOTALL       // Matching a `.` will not exclude newlines.
	MultiLine       CompileFlag = C.HS_FLAG_MULTILINE    // Set multi-line anchoring.
	SingleMatch     CompileFlag = C.HS_FLAG_SINGLEMATCH  // Set single-match only mode.
	AllowEmpty      CompileFlag = C.HS_FLAG_ALLOWEMPTY   // Allow expressions that can match against empty buffers.
	Utf8Mode        CompileFlag = C.HS_FLAG_UTF8         // Enable UTF-8 mode for this expression.
	UnicodeProperty CompileFlag = C.HS_FLAG_UCP          // Enable Unicode property support for this expression.
	PrefilterMode   CompileFlag = C.HS_FLAG_PREFILTER    // Enable prefiltering mode for this expression.
	SomLeftMost     CompileFlag = C.HS_FLAG_SOM_LEFTMOST // Enable leftmost start of match reporting.
)

var compileFlags = map[rune]CompileFlag{
	'i': Caseless,
	's': DotAll,
	'm': MultiLine,
	'H': SingleMatch,
	'V': AllowEmpty,
	'8': Utf8Mode,
	'W': UnicodeProperty,
	'P': PrefilterMode,
	'L': SomLeftMost,
}

var deprecatedCompileFlags = map[rune]CompileFlag{
	'o': SingleMatch,
	'e': AllowEmpty,
	'u': Utf8Mode,
	'p': UnicodeProperty,
	'f': PrefilterMode,
	'l': SomLeftMost,
}

/*
Parse the compile pattern flags from string

	i	Caseless 		Case-insensitive matching
	s	DotAll			Dot (.) will match newlines
	m	MultiLine		Multi-line anchoring
	H	SingleMatch		Report match ID at most once (`o` deprecated)
	V	AllowEmpty		Allow patterns that can match against empty buffers (`e` deprecated)
	8	Utf8Mode		UTF-8 mode (`u` deprecated)
	W	UnicodeProperty		Unicode property support (`p` deprecated)
	P	PrefilterMode		Prefiltering mode (`f` deprecated)
	L	SomLeftMost		Leftmost start of match reporting (`l` deprecated)
	C	Combination		Logical combination of patterns (Hyperscan 5.0)
	Q	Quiet			Quiet at matching (Hyperscan 5.0)
*/
func ParseCompileFlag(s string) (CompileFlag, error) {
	var flags CompileFlag

	for _, c := range s {
		if flag, exists := compileFlags[c]; exists {
			flags |= flag
		} else if flag, exists := deprecatedCompileFlags[c]; exists {
			flags |= flag
		} else {
			return 0, fmt.Errorf("unknown flag `%c`", c)
		}
	}

	return flags, nil
}

func (flags CompileFlag) String() string {
	var values []string

	for c, flag := range compileFlags {
		if (flags & flag) == flag {
			values = append(values, string(c))
		}
	}

	sort.Strings(values)

	return strings.Join(values, "")
}

// CpuFeature is the CPU feature support flags
type CpuFeature int

const (
	AVX2   CpuFeature = C.HS_CPU_FEATURES_AVX2   // AVX2 is a CPU features flag indicates that the target platform supports AVX2 instructions.
	AVX512 CpuFeature = C.HS_CPU_FEATURES_AVX512 // AVX512 is a CPU features flag indicates that the target platform supports AVX512 instructions, specifically AVX-512BW. Using AVX512 implies the use of AVX2.
)

// TuneFlag is the tuning flags
type TuneFlag int

const (
	Generic       TuneFlag = C.HS_TUNE_FAMILY_GENERIC // Generic indicates that the compiled database should not be tuned for any particular target platform.
	SandyBridge   TuneFlag = C.HS_TUNE_FAMILY_SNB     // SandyBridge indicates that the compiled database should be tuned for the Sandy Bridge microarchitecture.
	IvyBridge     TuneFlag = C.HS_TUNE_FAMILY_IVB     // IvyBridge indicates that the compiled database should be tuned for the Ivy Bridge microarchitecture.
	Haswell       TuneFlag = C.HS_TUNE_FAMILY_HSW     // Haswell indicates that the compiled database should be tuned for the Haswell microarchitecture.
	Silvermont    TuneFlag = C.HS_TUNE_FAMILY_SLM     // Silvermont indicates that the compiled database should be tuned for the Silvermont microarchitecture.
	Broadwell     TuneFlag = C.HS_TUNE_FAMILY_BDW     // Broadwell indicates that the compiled database should be tuned for the Broadwell microarchitecture.
	Skylake       TuneFlag = C.HS_TUNE_FAMILY_SKL     // Skylake indicates that the compiled database should be tuned for the Skylake microarchitecture.
	SkylakeServer TuneFlag = C.HS_TUNE_FAMILY_SKX     // SkylakeServer indicates that the compiled database should be tuned for the Skylake Server microarchitecture.
	Goldmont      TuneFlag = C.HS_TUNE_FAMILY_GLM     // Goldmont indicates that the compiled database should be tuned for the Goldmont microarchitecture.
)

// Compile mode flags
type ModeFlag uint

const (
	// BlockMode for the block scan (non-streaming) database.
	BlockMode ModeFlag = C.HS_MODE_BLOCK
	// NoStreamMode is alias for Block.
	NoStreamMode ModeFlag = C.HS_MODE_NOSTREAM
	// StreamMode for the streaming database.
	StreamMode ModeFlag = C.HS_MODE_STREAM
	// VectoredMode for the vectored scanning database.
	VectoredMode ModeFlag = C.HS_MODE_VECTORED
	// SomHorizonLargeMode use full precision to track start of match offsets in stream state.
	SomHorizonLargeMode ModeFlag = C.HS_MODE_SOM_HORIZON_LARGE
	// SomHorizonMediumMode use medium precision to track start of match offsets in stream state. (within 2^32 bytes)
	SomHorizonMediumMode ModeFlag = C.HS_MODE_SOM_HORIZON_MEDIUM
	// SomHorizonSmallMode use limited precision to track start of match offsets in stream state. (within 2^16 bytes)
	SomHorizonSmallMode ModeFlag = C.HS_MODE_SOM_HORIZON_SMALL
)

const ModeMask ModeFlag = 0xFF

var modeFlags = map[string]ModeFlag{
	"STREAM":   StreamMode,
	"NOSTREAM": BlockMode,
	"VECTORED": VectoredMode,
	"BLOCK":    BlockMode,
}

func ParseModeFlag(s string) (ModeFlag, error) {
	if mode, exists := modeFlags[strings.ToUpper(s)]; exists {
		return mode, nil
	}

	return BlockMode, errors.New("Unknown Mode: " + s)
}

func (m ModeFlag) String() string {
	switch m & 0xF {
	case BlockMode:
		return "BLOCK"
	case StreamMode:
		return "STREAM"
	case VectoredMode:
		return "VECTORED"
	default:
		panic(fmt.Sprintf("unknown mode: %d", m))
	}
}

type ScanFlag uint

type HsError int

const (
	ErrSuccess               HsError = C.HS_SUCCESS           // ErrSuccess is the error returned if the engine completed normally.
	ErrInvalid               HsError = C.HS_INVALID           // ErrInvalid is the error returned if a parameter passed to this function was invalid.
	ErrNoMemory              HsError = C.HS_NOMEM             // ErrNoMemory is the error returned if a memory allocation failed.
	ErrScanTerminated        HsError = C.HS_SCAN_TERMINATED   // ErrScanTerminated is the error returned if the engine was terminated by callback.
	ErrCompileError          HsError = C.HS_COMPILER_ERROR    // ErrCompileError is the error returned if the pattern compiler failed.
	ErrDatabaseVersionError  HsError = C.HS_DB_VERSION_ERROR  // ErrDatabaseVersionError is the error returned if the given database was built for a different version of Hyperscan.
	ErrDatabasePlatformError HsError = C.HS_DB_PLATFORM_ERROR // ErrDatabasePlatformError is the error returned if the given database was built for a different platform (i.e., CPU type).
	ErrDatabaseModeError     HsError = C.HS_DB_MODE_ERROR     // ErrDatabaseModeError is the error returned if the given database was built for a different mode of operation.
	ErrBadAlign              HsError = C.HS_BAD_ALIGN         // ErrBadAlign is the error returned if a parameter passed to this function was not correctly aligned.
	ErrBadAlloc              HsError = C.HS_BAD_ALLOC         // ErrBadAlloc is the error returned if the memory allocator did not correctly return memory suitably aligned.
	ErrScratchInUse          HsError = C.HS_SCRATCH_IN_USE    // ErrScratchInUse is the error returned if the scratch region was already in use.
	ErrArchError             HsError = C.HS_ARCH_ERROR        // ErrArchError is the error returned if unsupported CPU architecture.
)

var (
	hsErrorMessages = map[HsError]string{
		C.HS_SUCCESS:           "The engine completed normally.",
		C.HS_INVALID:           "A parameter passed to this function was invalid.",
		C.HS_NOMEM:             "A memory allocation failed.",
		C.HS_SCAN_TERMINATED:   "The engine was terminated by callback.",
		C.HS_COMPILER_ERROR:    "The pattern compiler failed.",
		C.HS_DB_VERSION_ERROR:  "The given database was built for a different version of Hyperscan.",
		C.HS_DB_PLATFORM_ERROR: "The given database was built for a different platform (i.e., CPU type).",
		C.HS_DB_MODE_ERROR:     "The given database was built for a different mode of operation.",
		C.HS_BAD_ALIGN:         "A parameter passed to this function was not correctly aligned.",
		C.HS_BAD_ALLOC:         "The memory allocator did not correctly return aligned memory.",
		C.HS_SCRATCH_IN_USE:    "The scratch region was already in use.",
		C.HS_ARCH_ERROR:        "Unsupported CPU architecture.",
	}
)

func (e HsError) Error() string {
	if msg, exists := hsErrorMessages[e]; exists {
		return msg
	}

	return fmt.Sprintf("unexpected error, %d", int(e))
}

type compileError struct {
	msg  string
	expr int
}

// A human-readable error message describing the error.
func (e *compileError) Error() string { return e.msg }

// The zero-based number of the expression that caused the error (if this can be determined).
// If the error is not specific to an expression, then this value will be less than zero.
func (e *compileError) Expression() int { return e.expr }

type hsPlatformInfo struct {
	platform C.struct_hs_platform_info
}

func (i *hsPlatformInfo) Tune() TuneFlag { return TuneFlag(i.platform.tune) }

func (i *hsPlatformInfo) CpuFeatures() CpuFeature { return CpuFeature(i.platform.cpu_features) }

func newPlatformInfo(tune TuneFlag, cpu CpuFeature) *hsPlatformInfo {
	var platform C.struct_hs_platform_info

	platform.tune = C.uint(tune)
	platform.cpu_features = C.ulonglong(cpu)

	return &hsPlatformInfo{platform}
}

func hsPopulatePlatform() (*hsPlatformInfo, error) {
	var platform C.struct_hs_platform_info

	if ret := C.hs_populate_platform(&platform); ret != C.HS_SUCCESS {
		return nil, HsError(ret)
	}

	return &hsPlatformInfo{platform}, nil
}

type hsDatabase *C.hs_database_t
type hsScratch *C.hs_scratch_t
type hsStream *C.hs_stream_t

// A type containing information related to an expression
type ExprInfo struct {
	MinWidth        uint // The minimum length in bytes of a match for the pattern.
	MaxWidth        uint // The maximum length in bytes of a match for the pattern.
	ReturnUnordered bool // Whether this expression can produce matches that are not returned in order, such as those produced by assertions.
	AtEndOfData     bool // Whether this expression can produce matches at end of data (EOD).
	OnlyAtEndOfData bool // Whether this expression can *only* produce matches at end of data (EOD).
}

// If the pattern expression has an unbounded maximum width
const UnboundedMaxWidth = C.UINT_MAX

func newExprInfo(info *C.hs_expr_info_t) *ExprInfo {
	return &ExprInfo{
		MinWidth:        uint(info.min_width),
		MaxWidth:        uint(info.max_width),
		ReturnUnordered: info.unordered_matches != 0,
		AtEndOfData:     info.matches_at_eod != 0,
		OnlyAtEndOfData: info.matches_only_at_eod != 0,
	}
}

// ExprExt is a structure containing additional parameters related to an expression.
type ExprExt struct {
	Flags           ExtFlag // Flags governing which parts of this structure are to be used by the compiler.
	MinOffset       uint    // The minimum end offset in the data stream at which this expression should match successfully.
	MaxOffset       uint    // The maximum end offset in the data stream at which this expression should match successfully.
	MinLength       uint    // The minimum match length (from start to end) required to successfully match this expression.
	EditDistance    uint    // Allow patterns to approximately match within this edit distance.
	HammingDistance uint    // Allow patterns to approximately match within this Hamming distance.
}

func (ext *ExprExt) String() string {
	var values []string

	if (ext.Flags & MinOffset) == MinOffset {
		values = append(values, fmt.Sprintf("min_offset=%d", ext.MinOffset))
	}
	if (ext.Flags & MaxOffset) == MaxOffset {
		values = append(values, fmt.Sprintf("max_offset=%d", ext.MaxOffset))
	}
	if (ext.Flags & MinLength) == MinLength {
		values = append(values, fmt.Sprintf("min_length=%d", ext.MinLength))
	}
	if (ext.Flags & EditDistance) == EditDistance {
		values = append(values, fmt.Sprintf("edit_distance=%d", ext.EditDistance))
	}
	if (ext.Flags & HammingDistance) == HammingDistance {
		values = append(values, fmt.Sprintf("hamming_distance=%d", ext.HammingDistance))
	}

	return "{" + strings.Join(values, ",") + "}"
}

func (ext *ExprExt) new_expr_ext() *C.hs_expr_ext_t {
	e := (*C.hs_expr_ext_t)(C.calloc(1, C.size_t(unsafe.Sizeof(C.hs_expr_ext_t{}))))

	e.flags = C.ulonglong(ext.Flags)
	e.min_offset = C.ulonglong(ext.MinOffset)
	e.max_offset = C.ulonglong(ext.MaxOffset)
	e.min_length = C.ulonglong(ext.MinLength)
	e.edit_distance = C.uint(ext.EditDistance)
	e.hamming_distance = C.uint(ext.HammingDistance)

	return e
}

func newExprExt(ext *C.hs_expr_ext_t) *ExprExt {
	e := new(ExprExt)

	if (ext.flags & C.HS_EXT_FLAG_MIN_OFFSET) == C.HS_EXT_FLAG_MIN_OFFSET {
		e.Flags |= MinOffset
		e.MinOffset = uint(ext.min_offset)
	}
	if (ext.flags & C.HS_EXT_FLAG_MAX_OFFSET) == C.HS_EXT_FLAG_MAX_OFFSET {
		e.Flags |= MaxOffset
		e.MaxOffset = uint(ext.max_offset)
	}
	if (ext.flags & C.HS_EXT_FLAG_MIN_LENGTH) == C.HS_EXT_FLAG_MIN_LENGTH {
		e.Flags |= MinLength
		e.MinLength = uint(ext.min_length)
	}
	if (ext.flags & C.HS_EXT_FLAG_EDIT_DISTANCE) == C.HS_EXT_FLAG_EDIT_DISTANCE {
		e.Flags |= EditDistance
		e.EditDistance = uint(ext.edit_distance)
	}
	if (ext.flags & C.HS_EXT_FLAG_HAMMING_DISTANCE) == C.HS_EXT_FLAG_HAMMING_DISTANCE {
		e.Flags |= HammingDistance
		e.HammingDistance = uint(ext.hamming_distance)
	}

	return e
}

func ParseExprExt(s string) (ext *ExprExt, err error) {
	ext = new(ExprExt)

	if strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}") {
		s = strings.TrimSuffix(strings.TrimPrefix(s, "{"), "}")
	}

	for _, s := range strings.Split(s, ",") {
		parts := strings.SplitN(s, "=", 2)

		if len(parts) != 2 {
			continue
		}

		key := parts[0]
		value := parts[1]

		var n uint64
		n, err = strconv.ParseUint(value, 10, 64)
		if err != nil {
			return
		}

		if key == "min_offset" {
			ext.Flags |= MinOffset
			ext.MinOffset = uint(n)
		} else if key == "max_offset" {
			ext.Flags |= MaxOffset
			ext.MaxOffset = uint(n)
		} else if key == "min_length" {
			ext.Flags |= MinLength
			ext.MinLength = uint(n)
		} else if key == "edit_distance" {
			ext.Flags |= EditDistance
			ext.EditDistance = uint(n)
		} else if key == "hamming_distance" {
			ext.Flags |= HammingDistance
			ext.HammingDistance = uint(n)
		}
	}

	return
}

type hsAllocFunc func(uint) unsafe.Pointer
type hsFreeFunc func(unsafe.Pointer)

type hsAllocator struct {
	Alloc hsAllocFunc
	Free  hsFreeFunc
}

var (
	dbAllocator      hsAllocator
	miscAllocator    hsAllocator
	scratchAllocator hsAllocator
	streamAllocator  hsAllocator
)

func hsDefaultAlloc(size uint) unsafe.Pointer {
	return C.aligned64_malloc(C.size_t(size))
}

func hsDefaultFree(ptr unsafe.Pointer) {
	C.aligned64_free(ptr)
}

//export hsDbAlloc
func hsDbAlloc(size C.size_t) unsafe.Pointer {
	if dbAllocator.Alloc != nil {
		return dbAllocator.Alloc(uint(size))
	}

	return hsDefaultAlloc(uint(size))
}

//export hsDbFree
func hsDbFree(ptr unsafe.Pointer) {
	if dbAllocator.Free != nil {
		dbAllocator.Free(ptr)
	} else {
		hsDefaultFree(ptr)
	}
}

func hsSetDatabaseAllocator(allocFunc hsAllocFunc, freeFunc hsFreeFunc) error {
	dbAllocator = hsAllocator{allocFunc, freeFunc}

	if ret := C.hs_set_database_allocator_cgo(); ret != C.HS_SUCCESS {
		return HsError(ret)
	}

	return nil
}

func hsClearDatabaseAllocator() error {
	if ret := C.hs_clear_database_allocator_cgo(); ret != C.HS_SUCCESS {
		return HsError(ret)
	}

	return nil
}

//export hsMiscAlloc
func hsMiscAlloc(size C.size_t) unsafe.Pointer {
	if miscAllocator.Alloc != nil {
		return miscAllocator.Alloc(uint(size))
	}

	return hsDefaultAlloc(uint(size))
}

//export hsMiscFree
func hsMiscFree(ptr unsafe.Pointer) {
	if miscAllocator.Free != nil {
		miscAllocator.Free(ptr)
	} else {
		hsDefaultFree(ptr)
	}
}

func hsSetMiscAllocator(allocFunc hsAllocFunc, freeFunc hsFreeFunc) error {
	miscAllocator = hsAllocator{allocFunc, freeFunc}

	if ret := C.hs_set_misc_allocator_cgo(); ret != C.HS_SUCCESS {
		return HsError(ret)
	}

	return nil
}

func hsClearMiscAllocator() error {
	if ret := C.hs_clear_misc_allocator_cgo(); ret != C.HS_SUCCESS {
		return HsError(ret)
	}

	return nil
}

//export hsScratchAlloc
func hsScratchAlloc(size C.size_t) unsafe.Pointer {
	if scratchAllocator.Alloc != nil {
		return scratchAllocator.Alloc(uint(size))
	}

	return hsDefaultAlloc(uint(size))
}

//export hsScratchFree
func hsScratchFree(ptr unsafe.Pointer) {
	if scratchAllocator.Free != nil {
		scratchAllocator.Free(ptr)
	} else {
		hsDefaultFree(ptr)
	}
}

func hsSetScratchAllocator(allocFunc hsAllocFunc, freeFunc hsFreeFunc) error {
	scratchAllocator = hsAllocator{allocFunc, freeFunc}

	if ret := C.hs_set_scratch_allocator_cgo(); ret != C.HS_SUCCESS {
		return HsError(ret)
	}

	return nil
}

func hsClearScratchAllocator() error {
	if ret := C.hs_clear_scratch_allocator_cgo(); ret != C.HS_SUCCESS {
		return HsError(ret)
	}

	return nil
}

//export hsStreamAlloc
func hsStreamAlloc(size C.size_t) unsafe.Pointer {
	if streamAllocator.Alloc != nil {
		return streamAllocator.Alloc(uint(size))
	}

	return hsDefaultAlloc(uint(size))
}

//export hsStreamFree
func hsStreamFree(ptr unsafe.Pointer) {
	if streamAllocator.Free != nil {
		streamAllocator.Free(ptr)
	} else {
		hsDefaultFree(ptr)
	}
}

func hsSetStreamAllocator(allocFunc hsAllocFunc, freeFunc hsFreeFunc) error {
	streamAllocator = hsAllocator{allocFunc, freeFunc}

	if ret := C.hs_set_stream_allocator_cgo(); ret != C.HS_SUCCESS {
		return HsError(ret)
	}

	return nil
}

func hsClearStreamAllocator() error {
	if ret := C.hs_clear_stream_allocator_cgo(); ret != C.HS_SUCCESS {
		return HsError(ret)
	}

	return nil
}

func hsVersion() string {
	return C.GoString(C.hs_version())
}

func hsValidPlatform() error {
	if ret := C.hs_valid_platform(); ret != C.HS_SUCCESS {
		return HsError(ret)
	}

	return nil
}

func hsFreeDatabase(db hsDatabase) error {
	if ret := C.hs_free_database(db); ret != C.HS_SUCCESS {
		return HsError(ret)
	}

	return nil
}

func hsSerializeDatabase(db hsDatabase) ([]byte, error) {
	var data *C.char
	var length C.size_t

	if ret := C.hs_serialize_database(db, &data, &length); ret != C.HS_SUCCESS {
		return nil, HsError(ret)
	}

	return C.GoBytes(unsafe.Pointer(data), C.int(length)), nil
}

func hsDeserializeDatabase(data []byte) (hsDatabase, error) {
	var db *C.hs_database_t

	ret := C.hs_deserialize_database((*C.char)(unsafe.Pointer(&data[0])), C.size_t(len(data)), &db)

	runtime.KeepAlive(data)

	if ret != C.HS_SUCCESS {
		return nil, HsError(ret)
	}

	return db, nil
}

func hsDeserializeDatabaseAt(data []byte, db hsDatabase) error {
	ret := C.hs_deserialize_database_at((*C.char)(unsafe.Pointer(&data[0])), C.size_t(len(data)), db)

	runtime.KeepAlive(data)

	if ret != C.HS_SUCCESS {
		return HsError(ret)
	}

	return nil
}

func hsStreamSize(db hsDatabase) (int, error) {
	var size C.size_t

	if ret := C.hs_stream_size(db, &size); ret != C.HS_SUCCESS {
		return 0, HsError(ret)
	}

	return int(size), nil
}

func hsDatabaseSize(db hsDatabase) (int, error) {
	var size C.size_t

	if ret := C.hs_database_size(db, &size); ret != C.HS_SUCCESS {
		return -1, HsError(ret)
	}

	return int(size), nil
}

func hsSerializedDatabaseSize(data []byte) (int, error) {
	var size C.size_t

	ret := C.hs_serialized_database_size((*C.char)(unsafe.Pointer(&data[0])), C.size_t(len(data)), &size)

	runtime.KeepAlive(data)

	if ret != C.HS_SUCCESS {
		return 0, HsError(ret)
	}

	return int(size), nil
}

func hsDatabaseInfo(db hsDatabase) (string, error) {
	var info *C.char

	if ret := C.hs_database_info(db, &info); ret != C.HS_SUCCESS {
		return "", HsError(ret)
	}

	return C.GoString(info), nil
}

func hsSerializedDatabaseInfo(data []byte) (string, error) {
	var info *C.char

	ret := C.hs_serialized_database_info((*C.char)(unsafe.Pointer(&data[0])), C.size_t(len(data)), &info)

	runtime.KeepAlive(data)

	if ret != C.HS_SUCCESS {
		return "", HsError(ret)
	}

	return C.GoString(info), nil
}

func hsCompile(expression string, flags CompileFlag, mode ModeFlag, info *hsPlatformInfo) (hsDatabase, error) {
	var db *C.hs_database_t
	var err *C.hs_compile_error_t
	var platform *C.hs_platform_info_t

	if info != nil {
		platform = &info.platform
	}

	expr := C.CString(expression)

	ret := C.hs_compile(expr, C.uint(flags), C.uint(mode), platform, &db, &err)

	C.free(unsafe.Pointer(expr))

	if ret == C.HS_SUCCESS {
		return db, nil
	}

	if err != nil {
		defer C.hs_free_compile_error(err)
	}

	if ret == C.HS_COMPILER_ERROR && err != nil {
		return nil, &compileError{C.GoString(err.message), int(err.expression)}
	}

	return nil, fmt.Errorf("compile error, %d", int(ret))
}

func hsCompileMulti(patterns []*Pattern, mode ModeFlag, info *hsPlatformInfo) (hsDatabase, error) {
	var db *C.hs_database_t
	var err *C.hs_compile_error_t
	var platform *C.hs_platform_info_t

	if info != nil {
		platform = &info.platform
	}

	cexprs := (**C.char)(C.calloc(C.size_t(len(patterns)), C.size_t(unsafe.Sizeof(uintptr(0)))))
	exprs := (*[1 << 30]*C.char)(unsafe.Pointer(cexprs))[:len(patterns):len(patterns)]

	cflags := (*C.uint)(C.calloc(C.size_t(len(patterns)), C.size_t(unsafe.Sizeof(C.uint(0)))))
	flags := (*[1 << 30]C.uint)(unsafe.Pointer(cflags))[:len(patterns):len(patterns)]

	cids := (*C.uint)(C.calloc(C.size_t(len(patterns)), C.size_t(unsafe.Sizeof(C.uint(0)))))
	ids := (*[1 << 30]C.uint)(unsafe.Pointer(cids))[:len(patterns):len(patterns)]

	cexts := (**C.hs_expr_ext_t)(C.calloc(C.size_t(len(patterns)), C.size_t(unsafe.Sizeof(uintptr(0)))))
	exts := (*[1 << 30]*C.hs_expr_ext_t)(unsafe.Pointer(cexts))[:len(patterns):len(patterns)]

	cexts := (**C.hs_expr_ext_t)(C.calloc(C.size_t(len(patterns)), C.size_t(unsafe.Sizeof(uintptr(0)))))
	exts := *(*[]*C.hs_expr_ext_t)(unsafe.Pointer(&reflect.SliceHeader{
		Data: uintptr(unsafe.Pointer(cexts)),
		Len:  len(patterns),
		Cap:  len(patterns),
	}))

	for i, pattern := range patterns {
		exprs[i] = C.CString(string(pattern.Expression))
		flags[i] = C.uint(pattern.Flags)
		ids[i] = C.uint(pattern.Id)
		if pattern.ext != nil {
			exts[i] = pattern.ext.new_expr_ext()
		}
	}

	ret := C.hs_compile_ext_multi(cexprs, cflags, cids, cexts, C.uint(len(patterns)), C.uint(mode), platform, &db, &err)

	for _, expr := range exprs {
		C.free(unsafe.Pointer(expr))
	}

	C.free(unsafe.Pointer(cexprs))
	C.free(unsafe.Pointer(cflags))
	C.free(unsafe.Pointer(cexts))
	C.free(unsafe.Pointer(cids))

	for _, ext := range exts {
		if ext != nil {
			C.free(unsafe.Pointer(ext))
		}
	}

	if err != nil {
		defer C.hs_free_compile_error(err)
	}

	if ret == C.HS_SUCCESS {
		return db, nil
	}

	if ret == C.HS_COMPILER_ERROR && err != nil {
		return nil, &compileError{C.GoString(err.message), int(err.expression)}
	}

	return nil, fmt.Errorf("compile error, %d", int(ret))
}

func hsExpressionInfo(expression string, flags CompileFlag) (*ExprInfo, error) {
	var info *C.hs_expr_info_t
	var err *C.hs_compile_error_t

	expr := C.CString(expression)
	defer C.free(unsafe.Pointer(expr))

	ret := C.hs_expression_info(expr, C.uint(flags), &info, &err)

	if ret == C.HS_SUCCESS && info != nil {
		defer hsMiscFree(unsafe.Pointer(info))

		return newExprInfo(info), nil
	}

	if err != nil {
		defer C.hs_free_compile_error(err)
	}

	if ret == C.HS_COMPILER_ERROR && err != nil {
		return nil, &compileError{C.GoString(err.message), int(err.expression)}
	}

	return nil, fmt.Errorf("compile error, %d", int(ret))
}

func hsExpressionExt(expression string, flags CompileFlag) (ext *ExprExt, info *ExprInfo, err error) {
	expr_ext := new(C.hs_expr_ext_t)
	var expr_info *C.hs_expr_info_t
	var compile_err *C.hs_compile_error_t

	expr := C.CString(expression)

	ret := C.hs_expression_ext_info(expr, C.uint(flags), expr_ext, &expr_info, &compile_err)

	C.free(unsafe.Pointer(expr))

	if ret == C.HS_SUCCESS && expr_info != nil {
		defer hsMiscFree(unsafe.Pointer(expr_info))

		ext = newExprExt(expr_ext)
		info = newExprInfo(expr_info)
	}

	if compile_err != nil {
		defer C.hs_free_compile_error(compile_err)
	}

	if ret == C.HS_COMPILER_ERROR && compile_err != nil {
		err = &compileError{C.GoString(compile_err.message), int(compile_err.expression)}
	} else {
		err = fmt.Errorf("compile error, %d", int(ret))
	}

	return
}

func hsAllocScratch(db hsDatabase) (hsScratch, error) {
	var scratch *C.hs_scratch_t

	if ret := C.hs_alloc_scratch(db, &scratch); ret != C.HS_SUCCESS {
		return nil, HsError(ret)
	}

	return scratch, nil
}

func hsReallocScratch(db hsDatabase, scratch *hsScratch) error {
	if ret := C.hs_alloc_scratch(db, (**C.struct_hs_scratch)(scratch)); ret != C.HS_SUCCESS {
		return HsError(ret)
	}

	return nil
}

func hsCloneScratch(scratch hsScratch) (hsScratch, error) {
	var clone *C.hs_scratch_t

	if ret := C.hs_clone_scratch(scratch, &clone); ret != C.HS_SUCCESS {
		return nil, HsError(ret)
	}

	return clone, nil
}

func hsScratchSize(scratch hsScratch) (int, error) {
	var size C.size_t

	if ret := C.hs_scratch_size(scratch, &size); ret != C.HS_SUCCESS {
		return 0, HsError(ret)
	}

	return int(size), nil
}

func hsFreeScratch(scratch hsScratch) error {
	if ret := C.hs_free_scratch(scratch); ret != C.HS_SUCCESS {
		return HsError(ret)
	}

	return nil
}

type hsMatchEventHandler func(id uint, from, to uint64, flags uint, context interface{}) error

type hsMatchEventContext struct {
	handler hsMatchEventHandler
	context interface{}
}

var matchEventContexts sync.Map

func newMatchEventContext(handler hsMatchEventHandler, context interface{}) (unsafe.Pointer, func()) {
	ctxt := &hsMatchEventContext{handler, context}
	ptr := unsafe.Pointer(ctxt)
	matchEventContexts.Store(ptr, ctxt)
	return ptr, func() { matchEventContexts.Delete(ptr) }
}

func extractMatchEventContext(data unsafe.Pointer) (hsMatchEventHandler, interface{}) {
	value, ok := matchEventContexts.Load(data)

	if !ok {
		panic(data)
	}

	ctxt, ok := value.(*hsMatchEventContext)

	if !ok && ctxt == nil {
		panic(value)
	}

	return ctxt.handler, ctxt.context
}

//export hsMatchEventCallback
func hsMatchEventCallback(id C.uint, from, to C.ulonglong, flags C.uint, data unsafe.Pointer) C.int {
	handler, context := extractMatchEventContext(data)

	err := handler(uint(id), uint64(from), uint64(to), uint(flags), context)

	if err != nil {
		return -1
	}

	return 0
}

func hsScan(db hsDatabase, data []byte, flags ScanFlag, scratch hsScratch, onEvent hsMatchEventHandler, context interface{}) error {
	if data == nil {
		return HsError(C.HS_INVALID)
	}

	ctxt, free := newMatchEventContext(onEvent, context)
	defer free()
	hdr := (*reflect.SliceHeader)(unsafe.Pointer(&data)) // FIXME: Zero-copy access to go data

	ret := C.hs_scan_cgo(db, (*C.char)(unsafe.Pointer(hdr.Data)), C.uint(hdr.Len),
		C.uint(flags), scratch, C.uintptr_t(uintptr(ctxt)))

	// Ensure go data is alive before the C function returns
	runtime.KeepAlive(data)
	runtime.KeepAlive(ctxt)

	if ret != C.HS_SUCCESS {
		return HsError(ret)
	}

	return nil
}

func hsScanVector(db hsDatabase, data [][]byte, flags ScanFlag, scratch hsScratch, onEvent hsMatchEventHandler, context interface{}) error {
	if data == nil {
		return HsError(C.HS_INVALID)
	}

	cdata := make([]uintptr, len(data))
	clength := make([]C.uint, len(data))

	for i, d := range data {
		if d == nil {
			return HsError(C.HS_INVALID)
		}

		hdr := (*reflect.SliceHeader)(unsafe.Pointer(&d)) // FIXME: Zero-copy access to go data
		cdata[i] = uintptr(unsafe.Pointer(hdr.Data))
		clength[i] = C.uint(hdr.Len)
	}

	ctxt, free := newMatchEventContext(onEvent, context)
	defer free()
	cdataHdr := (*reflect.SliceHeader)(unsafe.Pointer(&cdata))     // FIXME: Zero-copy access to go data
	clengthHdr := (*reflect.SliceHeader)(unsafe.Pointer(&clength)) // FIXME: Zero-copy access to go data

	ret := C.hs_scan_vector_cgo(db, (**C.char)(unsafe.Pointer(cdataHdr.Data)), (*C.uint)(unsafe.Pointer(clengthHdr.Data)),
		C.uint(cdataHdr.Len), C.uint(flags), scratch, C.uintptr_t(uintptr(ctxt)))

	// Ensure go data is alive before the C function returns
	runtime.KeepAlive(data)
	runtime.KeepAlive(cdata)
	runtime.KeepAlive(clength)
	runtime.KeepAlive(ctxt)

	if ret != C.HS_SUCCESS {
		return HsError(ret)
	}

	return nil
}

func hsOpenStream(db hsDatabase, flags ScanFlag) (hsStream, error) {
	var stream *C.hs_stream_t

	if ret := C.hs_open_stream(db, C.uint(flags), &stream); ret != C.HS_SUCCESS {
		return nil, HsError(ret)
	}

	return stream, nil
}

func hsScanStream(stream hsStream, data []byte, flags ScanFlag, scratch hsScratch, onEvent hsMatchEventHandler, context interface{}) error {
	if data == nil {
		return HsError(C.HS_INVALID)
	}

	ctxt, free := newMatchEventContext(onEvent, context)
	defer free()
	hdr := (*reflect.SliceHeader)(unsafe.Pointer(&data)) // FIXME: Zero-copy access to go data

	ret := C.hs_scan_stream_cgo(stream, (*C.char)(unsafe.Pointer(hdr.Data)), C.uint(hdr.Len),
		C.uint(flags), scratch, C.uintptr_t(uintptr(ctxt)))

	// Ensure go data is alive before the C function returns
	runtime.KeepAlive(data)
	runtime.KeepAlive(ctxt)

	if ret != C.HS_SUCCESS {
		return HsError(ret)
	}

	return nil
}

func hsCloseStream(stream hsStream, scratch hsScratch, onEvent hsMatchEventHandler, context interface{}) error {
	ctxt, free := newMatchEventContext(onEvent, context)
	defer free()

	ret := C.hs_close_stream_cgo(stream, scratch, C.uintptr_t(uintptr(ctxt)))

	runtime.KeepAlive(ctxt)

	if ret != C.HS_SUCCESS {
		return HsError(ret)
	}

	return nil
}

func hsResetStream(stream hsStream, flags ScanFlag, scratch hsScratch, onEvent hsMatchEventHandler, context interface{}) error {
	ctxt, free := newMatchEventContext(onEvent, context)
	defer free()

	ret := C.hs_reset_stream_cgo(stream, C.uint(flags), scratch, C.uintptr_t(uintptr(ctxt)))

	runtime.KeepAlive(ctxt)

	if ret != C.HS_SUCCESS {
		return HsError(ret)
	}

	return nil
}

func hsCopyStream(stream hsStream) (hsStream, error) {
	var copied *C.hs_stream_t

	if ret := C.hs_copy_stream(&copied, stream); ret != C.HS_SUCCESS {
		return nil, HsError(ret)
	}

	return copied, nil
}

func hsResetAndCopyStream(to, from hsStream, scratch hsScratch, onEvent hsMatchEventHandler, context interface{}) error {
	ctxt, free := newMatchEventContext(onEvent, context)
	defer free()

	ret := C.hs_reset_and_copy_stream_cgo(to, from, scratch, C.uintptr_t(uintptr(ctxt)))

	runtime.KeepAlive(ctxt)

	if ret != C.HS_SUCCESS {
		return HsError(ret)
	}

	return nil
}

func hsCompressStream(stream hsStream, buf []byte) ([]byte, error) {
	var size C.size_t

	ret := C.hs_compress_stream(stream, (*C.char)(unsafe.Pointer(&buf[0])), C.size_t(len(buf)), &size)

	if ret == C.HS_INSUFFICIENT_SPACE {
		buf = make([]byte, size)

		ret = C.hs_compress_stream(stream, (*C.char)(unsafe.Pointer(&buf[0])), C.size_t(len(buf)), &size)
	}

	if ret != C.HS_SUCCESS {
		return nil, HsError(ret)
	}

	return buf[:size], nil
}

func hsExpandStream(db hsDatabase, stream *hsStream, buf []byte) error {
	ret := C.hs_expand_stream(db, (**C.hs_stream_t)(stream), (*C.char)(unsafe.Pointer(&buf[0])), C.size_t(len(buf)))

	runtime.KeepAlive(buf)

	if ret != C.HS_SUCCESS {
		return HsError(ret)
	}

	return nil
}

func hsResetAndExpandStream(stream hsStream, buf []byte, scratch hsScratch, onEvent hsMatchEventHandler, context interface{}) error {
	ctxt, free := newMatchEventContext(onEvent, context)
	defer free()

	ret := C.hs_reset_and_expand_stream_cgo(stream, (*C.char)(unsafe.Pointer(&buf[0])), C.uint(len(buf)),
		scratch, C.uintptr_t(uintptr(ctxt)))

	runtime.KeepAlive(buf)
	runtime.KeepAlive(ctxt)

	if ret != C.HS_SUCCESS {
		return HsError(ret)
	}

	return nil
}
