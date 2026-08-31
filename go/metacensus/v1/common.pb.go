// The MetaCensus API contract: shared metadata types.
//
// Protobuf is the type system here, not the transport. Every endpoint under
// `/metacensus/api/v1` speaks JSON produced by `protojson` — never protobuf
// binary — so the payloads stay curl-able. That has three consequences visible
// throughout these files:
//
//   * Field names are `snake_case` in proto and `lowerCamelCase` on the wire,
//     which is what the SPA already sends and receives. No `json_name`
//     overrides anywhere.
//   * Enums serialise as their value name, so the value names are PascalCase
//     (`Statement`, not `STATEMENT`) and the zero value is always
//     `Unspecified`.
//   * `google.protobuf.Timestamp` serialises as RFC 3339, which is what both
//     backends already emit for `created` and `lastCast`.
//
// There are no `service` definitions. Without transport annotations a service
// block would describe nothing real, so each request/response message names its
// HTTP method and path in a leading comment instead. That is the routing table.
//
//
// WHAT IS IN THIS CONTRACT, AND WHY SO LITTLE
//
// This is a first pass, and its bar is: only what we are confident we want.
// A field is easy to add later and very hard to remove once something depends
// on it, so uncertainty resolves to leaving it out. Arriving at a field in a
// small later change is also what attaches a reason to it; arriving at forty
// fields at once attaches a reason to none of them.
//
// The three sources disagree, and they are NOT weighted equally:
//
//   * infra (Go API + chaincode) is the design authority. It was written
//     deliberately and largely by hand. It implements less.
//   * demo (Bun + Elysia) was written rapidly with AI assistance. It is
//     authoritative about WHAT FEATURES EXIST — it is the only place papers,
//     protocols and extraction exist at all — and not about how they should be
//     shaped.
//   * types/*.ts says what the client currently declares, which is sometimes
//     neither of the above and occasionally describes a field no backend sends.
//
// So where demo deviates from infra, the question asked was not "which is
// deployed?" but "what is the right representation?" — and several fields came
// out because the honest answer was "we do not know yet".
//
// Two failure modes drove most of the removals, and they are worth naming
// because they recur:
//
//   1. A field nothing maintains is worse than a missing field, because it
//      lies. `updatedAt` that no writer updates, a `status` enum with five
//      values of which one can ever occur, a presigned URL cached in a column
//      until it expires. These are removed regardless of which source they came
//      from — including infra's own `lastActive`/`lastCredits`, which are set
//      once at creation and never touched again.
//   2. A field that is an artifact of one implementation rather than of the
//      domain. ORM junction wrappers, denormalised convenience lists, an id on
//      a record whose natural key is a pair.
//
// Every removal is recorded in a `REMOVED IN THE FIRST PASS` block in the file
// it would have belonged to, with the reason and the question it becomes.
// Nothing was dropped silently.
//
//
// ENDPOINTS EXCLUDED, recorded here because they are otherwise invisible.
//
//   * `POST /lit-search` (demo). NCBI `esummary` passed through verbatim. Its
//     `result` is a map keyed by PMID whose values are heterogeneous — every
//     key is an article object except `uids`, which is an array of strings — so
//     no `map<>` can hold it. Its forty-odd field names are
//     lowercase-no-separator (`sorttitle`, `nlmuniqueid`) which protojson's
//     lowerCamelCase would misspell. And it is NCBI's schema, not ours: a
//     change at NCBI is not a change to this contract.
//   * `GET /lit-search/{id}/abstract` (demo). Modellable on its own — demo
//     already projects the XML down to a list of strings — but it is half of a
//     feature whose other half is not, and contracting half a feature is worse
//     than contracting none of it. Literature search wants a MetaCensus-shaped
//     projection of NCBI (the SPA reads about eight of NCBI's forty fields);
//     both endpoints should be designed together at that point.
//   * `POST /domain` and `POST /category` (demo). Flat `{id, name}`
//     vocabularies with no infra counterpart — not even a stub route. They are
//     excluded together with the `Topic` fields that referenced them
//     (`domain`, `topicCategories`), because a taxonomy endpoint whose only
//     consumers are also removed describes nothing. Topic taxonomy is one
//     design question, not three.
//   * `POST /protocol` (demo). Declared in `src/lib/routes.ts`, served by demo,
//     called by nothing. Its handler queries the *topic* table with a protocol
//     join, so "list protocols" actually returns topics — and
//     `GET /topic/{topicId}/protocol` already covers the real need. Excluded as
//     misnamed rather than modelled as-is.
//   * `GET /topic/{topicId}/my-votes` (demo). Returns a bare array of prop ids
//     the caller has voted on; the SPA uses it to avoid a request per prop. The
//     need is real and the shape is not: it is a denormalised projection with
//     no infra counterpart. Two better shapes to choose between when this is
//     designed — carry the caller's own vote on each `Prop` in the list, or
//     make it a filter on the existing vote collection.
//   * `POST /metacensus/api/v1/fabric/execute/…` and `…/fabric/metadata/…`
//     (infra). Raw Hyperledger Fabric passthrough returning opaque chaincode
//     bytes, gated behind `ENABLE_FABRIC_DEBUG_ENDPOINTS`, and described by
//     infra's own comment as something that "should never be exposed on a
//     public internet-facing API". A debug hatch is not part of a client
//     contract.
//   * The `multipart/form-data` branch of `POST /paper/create` (demo). See
//     paper.proto.
//   * The `key` field of the sign-up body. See `SignUpRequest` in user.proto.
//   * `/metacensus/api/v1/auth` and `/metacensus/api/v1/org` — infra declares
//     path constants for both (`routeAuth`, carrying a `// TODO, do we need
//     this?`, and `routeOrg` / `routeOrgId`) and mounts neither. `Org` exists
//     as a chaincode type and contract with working Create and Get
//     transactions, so organisations are a real domain concept with no HTTP
//     surface at all. Left out because there is no endpoint to describe, not
//     because the concept is unwanted — and worth settling before anything
//     else grows a grouping concept, since demo's unreachable `group` tables
//     already overlap it.
//
// Out of scope by instruction rather than by judgement: the public backend in
// `server/`, served at `/metacensus/public/*`. Different prefix, different
// service, separate contract if it needs one.

// Code generated by protoc-gen-go. DO NOT EDIT.
// versions:
// 	protoc-gen-go v1.36.9
// 	protoc        (unknown)
// source: metacensus/v1/common.proto

package metacensusv1

import (
	protoreflect "google.golang.org/protobuf/reflect/protoreflect"
	protoimpl "google.golang.org/protobuf/runtime/protoimpl"
	reflect "reflect"
	sync "sync"
	unsafe "unsafe"
)

const (
	// Verify that this generated code is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(20 - protoimpl.MinVersion)
	// Verify that runtime/protoimpl is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(protoimpl.MaxVersion - 20)
)

// ListMetadata is the `metadata` half of every list response.
//
// It is empty, and that is deliberate rather than unfinished.
//
// Every list response is `{items, metadata}` — never a bare array — so that a
// list can grow a sibling field without breaking every consumer. `metadata` is
// where that growth goes, and pagination is what it is expected to carry. But
// the pagination model is not settled: demo takes `page`/`limit` three
// different ways (query string on `GET /topic` and `GET /user`, a JSON body
// field on `POST /paper`, `POST /extraction-review`, `POST /protocol-template`),
// echoes neither back, and computes no total; infra paginates nothing at all
// and carries a `// TODO Paginate this` on every one of its `GetAll` handlers.
//
// Declaring `page` and `limit` here would put two fields on the wire that no
// backend populates and that a cursor-based design would make wrong. The
// envelope is the part we are sure about; its contents are not.
//
// REMOVED IN THE FIRST PASS
//
//	page, limit — no backend echoes them; the model may not be offset-based.
//	total       — no backend computes one.
//
// Question: is pagination offset- or cursor-based, and do the parameters
// travel in the query string or the body?
type ListMetadata struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *ListMetadata) Reset() {
	*x = ListMetadata{}
	mi := &file_metacensus_v1_common_proto_msgTypes[0]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *ListMetadata) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*ListMetadata) ProtoMessage() {}

func (x *ListMetadata) ProtoReflect() protoreflect.Message {
	mi := &file_metacensus_v1_common_proto_msgTypes[0]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use ListMetadata.ProtoReflect.Descriptor instead.
func (*ListMetadata) Descriptor() ([]byte, []int) {
	return file_metacensus_v1_common_proto_rawDescGZIP(), []int{0}
}

// Error is the failure body. Both backends emit `{"error": "..."}`: demo from
// every handler's catch block, infra from `writeError`. One of the very few
// shapes all sources already agree on.
//
// SOURCE CONFLICT: the SPA reads `responseJson.error || responseJson.message`
// (`src/hooks/useFetchData.ts`), so it is prepared for a `message` field that
// no backend has ever emitted. The contract declares only `error`; the client's
// fallback is dead code.
//
// SOURCE CONFLICT: infra's `writeError` sets HTTP 500 for *every* failure,
// including bad input and missing records. demo returns 400/401/404/409/500 as
// appropriate. infra is wrong here; the status codes are demo's.
type Error struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	Error         string                 `protobuf:"bytes,1,opt,name=error,proto3" json:"error,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *Error) Reset() {
	*x = Error{}
	mi := &file_metacensus_v1_common_proto_msgTypes[1]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *Error) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*Error) ProtoMessage() {}

func (x *Error) ProtoReflect() protoreflect.Message {
	mi := &file_metacensus_v1_common_proto_msgTypes[1]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use Error.ProtoReflect.Descriptor instead.
func (*Error) Descriptor() ([]byte, []int) {
	return file_metacensus_v1_common_proto_rawDescGZIP(), []int{1}
}

func (x *Error) GetError() string {
	if x != nil {
		return x.Error
	}
	return ""
}

// HealthcheckResponse is the body of `GET /healthcheck`.
//
// infra serves this both at the root and under the v1 prefix and returns
// `{"status": "healthy"}`. demo has no healthcheck at all. Only the
// v1-prefixed route is in scope.
//
// The value is computed fresh per request rather than stored, so it cannot go
// stale — which is the whole test a field has to pass to be in here.
type HealthcheckResponse struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	Status        string                 `protobuf:"bytes,1,opt,name=status,proto3" json:"status,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *HealthcheckResponse) Reset() {
	*x = HealthcheckResponse{}
	mi := &file_metacensus_v1_common_proto_msgTypes[2]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *HealthcheckResponse) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*HealthcheckResponse) ProtoMessage() {}

func (x *HealthcheckResponse) ProtoReflect() protoreflect.Message {
	mi := &file_metacensus_v1_common_proto_msgTypes[2]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use HealthcheckResponse.ProtoReflect.Descriptor instead.
func (*HealthcheckResponse) Descriptor() ([]byte, []int) {
	return file_metacensus_v1_common_proto_rawDescGZIP(), []int{2}
}

func (x *HealthcheckResponse) GetStatus() string {
	if x != nil {
		return x.Status
	}
	return ""
}

var File_metacensus_v1_common_proto protoreflect.FileDescriptor

const file_metacensus_v1_common_proto_rawDesc = "" +
	"\n" +
	"\x1ametacensus/v1/common.proto\x12\rmetacensus.v1\"\x0e\n" +
	"\fListMetadata\"\x1d\n" +
	"\x05Error\x12\x14\n" +
	"\x05error\x18\x01 \x01(\tR\x05error\"-\n" +
	"\x13HealthcheckResponse\x12\x16\n" +
	"\x06status\x18\x01 \x01(\tR\x06statusBAZ?github.com/metacensus/ui/contract/go/metacensus/v1;metacensusv1b\x06proto3"

var (
	file_metacensus_v1_common_proto_rawDescOnce sync.Once
	file_metacensus_v1_common_proto_rawDescData []byte
)

func file_metacensus_v1_common_proto_rawDescGZIP() []byte {
	file_metacensus_v1_common_proto_rawDescOnce.Do(func() {
		file_metacensus_v1_common_proto_rawDescData = protoimpl.X.CompressGZIP(unsafe.Slice(unsafe.StringData(file_metacensus_v1_common_proto_rawDesc), len(file_metacensus_v1_common_proto_rawDesc)))
	})
	return file_metacensus_v1_common_proto_rawDescData
}

var file_metacensus_v1_common_proto_msgTypes = make([]protoimpl.MessageInfo, 3)
var file_metacensus_v1_common_proto_goTypes = []any{
	(*ListMetadata)(nil),        // 0: metacensus.v1.ListMetadata
	(*Error)(nil),               // 1: metacensus.v1.Error
	(*HealthcheckResponse)(nil), // 2: metacensus.v1.HealthcheckResponse
}
var file_metacensus_v1_common_proto_depIdxs = []int32{
	0, // [0:0] is the sub-list for method output_type
	0, // [0:0] is the sub-list for method input_type
	0, // [0:0] is the sub-list for extension type_name
	0, // [0:0] is the sub-list for extension extendee
	0, // [0:0] is the sub-list for field type_name
}

func init() { file_metacensus_v1_common_proto_init() }
func file_metacensus_v1_common_proto_init() {
	if File_metacensus_v1_common_proto != nil {
		return
	}
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: unsafe.Slice(unsafe.StringData(file_metacensus_v1_common_proto_rawDesc), len(file_metacensus_v1_common_proto_rawDesc)),
			NumEnums:      0,
			NumMessages:   3,
			NumExtensions: 0,
			NumServices:   0,
		},
		GoTypes:           file_metacensus_v1_common_proto_goTypes,
		DependencyIndexes: file_metacensus_v1_common_proto_depIdxs,
		MessageInfos:      file_metacensus_v1_common_proto_msgTypes,
	}.Build()
	File_metacensus_v1_common_proto = out.File
	file_metacensus_v1_common_proto_goTypes = nil
	file_metacensus_v1_common_proto_depIdxs = nil
}
