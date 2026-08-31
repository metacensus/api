package contract_test

import (
	"time"

	v1 "github.com/metacensus/ui/contract/go/metacensus/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// The fixtures below populate every message in the contract, once each, and the
// golden test marshals them and diffs the result against testdata/golden.
//
// They are written with the ids the two backends actually mint, deliberately
// mixed: prefixed UUIDs (`topc:0192a642-…`) for the resources infra owns, and
// stringified integer serials (`"17"`) for the ones only demo implements. Both
// arrive as JSON strings, which is the point — the contract cannot ratify
// either format, so it ratifies neither and requires strings.

const (
	topicID = "topc:0192a642-817d-7a3e-a282-d7a282ebd482"
	userID  = "user:0192a642-817d-7a3e-a282-d7a282ebd483"
	propID  = "prop:0192a642-817d-7a3e-a282-d7a282ebd485"

	// demo's serials, stringified. Papers, protocols and extractions exist only
	// in demo, so these are the ids a caller sees today.
	paperID      = "17"
	protocolID   = "4"
	sectionID    = "9"
	elementID    = "31"
	extractionID = "204"
	reviewID     = "58"
	templateID   = "3"
)

func ts(s string) *timestamppb.Timestamp {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return timestamppb.New(t)
}

func protocolElement() *v1.ProtocolElement {
	return &v1.ProtocolElement{
		Id:          elementID,
		Name:        "studyDesign",
		Type:        "radio",
		Label:       "Study design",
		Placeholder: "",
		Required:    true,
		Options: []*v1.ProtocolElementOption{
			{Name: "Randomised controlled trial", Value: "rct"},
			{Name: "Cohort", Value: "cohort"},
		},
		SortOrder: 1,
	}
}

func protocolSection() *v1.ProtocolSection {
	return &v1.ProtocolSection{
		Id:               sectionID,
		Title:            "Study characteristics",
		SortOrder:        0,
		ProtocolElements: []*v1.ProtocolElement{protocolElement()},
	}
}

func protocol() *v1.Protocol {
	return &v1.Protocol{
		Id:               protocolID,
		Title:            "DPYD extraction protocol",
		ProtocolSections: []*v1.ProtocolSection{protocolSection()},
	}
}

func user() *v1.User {
	return &v1.User{
		Id:      userID,
		Name:    "Ada Okonkwo",
		Email:   "ada@example.org",
		Country: "Nigeria",
		Created: ts("2024-09-01T10:00:00Z"),
	}
}

func topic() *v1.Topic {
	return &v1.Topic{
		Id:          topicID,
		Created:     ts("2024-10-19T13:56:05Z"),
		Name:        "DPYD Genotype and Fluoropyrimidine Toxicity",
		Description: "Association of DPYD genotype to fluoropyrimidine toxicity.",
	}
}

func vote() *v1.Vote {
	return &v1.Vote{
		PropId:      propID,
		UserId:      userID,
		Position:    v1.Vote_Against,
		Explanation: "The cited cohort excludes DPYD*2A heterozygotes.",
		Citations:   []*v1.PropCitation{{Start: 12, End: 48}},
		LastCast:    ts("2024-11-10T07:22:31Z"),
	}
}

func prop() *v1.Prop {
	return &v1.Prop{
		Id:          propID,
		AuthorId:    userID,
		Created:     ts("2024-11-05T14:02:00Z"),
		Type:        v1.Prop_Statement,
		Description: "DPYD genotype-guided dosing reduces grade 3+ toxicity.",
	}
}

func paper() *v1.Paper {
	return &v1.Paper{
		Id:       paperID,
		TopicId:  topicID,
		Title:    "DPYD genotype-guided dose individualisation of fluoropyrimidine therapy",
		Authors:  []string{"Linda M. Henricks", "Carin A. T. C. Lunenburg"},
		Abstract: "BACKGROUND: Fluoropyrimidines are widely used...",
		Doi:      "10.1016/S1470-2045(18)30686-7",
		Pmid:     "30348537",
		Created:  ts("2024-10-22T12:00:00Z"),
	}
}

func extractionReview() *v1.DataExtractionReview {
	return &v1.DataExtractionReview{
		Id:         reviewID,
		TopicId:    topicID,
		PaperId:    paperID,
		ProtocolId: protocolID,
		Created:    ts("2024-11-08T09:00:00Z"),
	}
}

func sourceLocation() []*v1.SourceLocationPage {
	return []*v1.SourceLocationPage{
		{Page: 3, Rects: []*v1.SourceRect{{X: 72.5, Y: 431.25, W: 268, H: 12.5}}},
	}
}

func dataExtraction() *v1.DataExtraction {
	return &v1.DataExtraction{
		Id:                     extractionID,
		DataExtractionReviewId: reviewID,
		PaperId:                paperID,
		ProtocolId:             protocolID,
		ProtocolElementId:      elementID,
		Data:                   "rct",
		SourceLocation:         sourceLocation(),
		Created:                ts("2024-11-08T09:12:00Z"),
	}
}

func extractionInput() *v1.DataExtractionInput {
	return &v1.DataExtractionInput{
		ProtocolElementId: elementID,
		ProtocolId:        protocolID,
		PaperId:           paperID,
		Data:              "rct",
		SourceLocation:    sourceLocation(),
	}
}

// listMetadata is set explicitly on every list fixture so the goldens show the
// envelope. Left unset it would be omitted entirely — a message field with
// nothing in it — and the `{items, metadata}` shape would be invisible in the
// very files that exist to demonstrate it.
func listMetadata() *v1.ListMetadata { return &v1.ListMetadata{} }

type fixture struct {
	name string
	msg  proto.Message
}

// fixtures covers every message declared in the contract. A message missing
// from this list fails TestEveryMessageHasAFixture.
func fixtures() []fixture {
	return []fixture{
		// common.proto
		{"ListMetadata", listMetadata()},
		{"Error", &v1.Error{Error: "Invalid credentials"}},
		{"HealthcheckResponse", &v1.HealthcheckResponse{Status: "healthy"}},

		// user.proto
		{"User", user()},
		{"LoginRequest", &v1.LoginRequest{Email: "ada@example.org", Password: "correct horse battery staple"}},
		{"SignUpRequest", &v1.SignUpRequest{
			Name:     "Ada Okonkwo",
			Email:    "ada@example.org",
			Country:  "Nigeria",
			Password: "correct horse battery staple",
		}},
		{"Session", &v1.Session{Token: "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.e30.signature"}},
		{"LogoutResponse", &v1.LogoutResponse{}},
		{"UserListRequest", &v1.UserListRequest{}},
		{"UserList", &v1.UserList{Items: []*v1.User{user()}, Metadata: listMetadata()}},
		{"UserGetRequest", &v1.UserGetRequest{UserId: userID}},

		// topic.proto
		{"Topic", topic()},
		{"TopicListRequest", &v1.TopicListRequest{}},
		{"TopicList", &v1.TopicList{Items: []*v1.Topic{topic()}, Metadata: listMetadata()}},
		{"TopicGetRequest", &v1.TopicGetRequest{TopicId: topicID}},
		{"TopicCreateRequest", &v1.TopicCreateRequest{
			Name:        "DPYD Genotype and Fluoropyrimidine Toxicity",
			Description: "Association of DPYD genotype to fluoropyrimidine toxicity.",
		}},
		{"TopicProtocolRequest", &v1.TopicProtocolRequest{TopicId: topicID}},
		{"Member", &v1.Member{Id: userID, Created: ts("2024-10-19T14:00:00Z")}},
		{"MemberListRequest", &v1.MemberListRequest{TopicId: topicID}},
		{"MemberList", &v1.MemberList{
			Items:    []*v1.Member{{Id: userID, Created: ts("2024-10-19T14:00:00Z")}},
			Metadata: listMetadata(),
		}},
		{"MemberGetRequest", &v1.MemberGetRequest{TopicId: topicID, UserId: userID}},

		// prop.proto
		{"Prop", prop()},
		{"Vote", vote()},
		{"PropCitation", &v1.PropCitation{Start: 12, End: 48}},
		{"PropListRequest", &v1.PropListRequest{TopicId: topicID}},
		{"PropList", &v1.PropList{Items: []*v1.Prop{prop()}, Metadata: listMetadata()}},
		{"PropGetRequest", &v1.PropGetRequest{TopicId: topicID, PropId: propID}},
		{"PropCreateRequest", &v1.PropCreateRequest{
			Type:        v1.Prop_TopicQuestion,
			Description: "Does DPYD genotype-guided dosing reduce severe toxicity?",
		}},
		{"VoteListRequest", &v1.VoteListRequest{TopicId: topicID, PropId: propID}},
		{"VoteList", &v1.VoteList{Items: []*v1.Vote{vote()}, Metadata: listMetadata()}},
		{"VoteSetRequest", &v1.VoteSetRequest{
			Position:    v1.Vote_Against,
			Explanation: "The cited cohort excludes DPYD*2A heterozygotes.",
			Citations:   []*v1.PropCitation{{Start: 12, End: 48}},
		}},

		// paper.proto
		{"Paper", paper()},
		{"PaperListRequest", &v1.PaperListRequest{TopicId: topicID, PaperId: ""}},
		{"PaperList", &v1.PaperList{Items: []*v1.Paper{paper()}, Metadata: listMetadata()}},
		{"PaperCreateRequest", &v1.PaperCreateRequest{
			TopicId:  topicID,
			Title:    "DPYD genotype-guided dose individualisation of fluoropyrimidine therapy",
			Authors:  []string{"Linda M. Henricks", "Carin A. T. C. Lunenburg"},
			Abstract: "BACKGROUND: Fluoropyrimidines are widely used...",
			Doi:      "10.1016/S1470-2045(18)30686-7",
			Pmid:     "30348537",
		}},
		{"PaperLookupRequest", &v1.PaperLookupRequest{Pmid: "30348537"}},
		{"PaperLookupResponse", &v1.PaperLookupResponse{
			Pmid:     "30348537",
			Title:    "DPYD genotype-guided dose individualisation of fluoropyrimidine therapy",
			Authors:  []string{"Linda M. Henricks", "Carin A. T. C. Lunenburg"},
			Abstract: "BACKGROUND: Fluoropyrimidines are widely used...",
			Doi:      "10.1016/S1470-2045(18)30686-7",
		}},
		{"PaperPresignedUrlRequest", &v1.PaperPresignedUrlRequest{PaperId: paperID}},
		{"PaperPresignedUrlResponse", &v1.PaperPresignedUrlResponse{
			Url: "https://s3.example.org/papers/17/henricks-2018.pdf?X-Amz-Expires=900",
		}},

		// protocol.proto
		{"Protocol", protocol()},
		{"ProtocolSection", protocolSection()},
		{"ProtocolElement", protocolElement()},
		{"ProtocolElementOption", &v1.ProtocolElementOption{Name: "Cohort", Value: "cohort"}},
		{"ProtocolTemplate", &v1.ProtocolTemplate{
			Id:               templateID,
			Title:            "Cochrane-style extraction template",
			ProtocolSections: []*v1.ProtocolSection{protocolSection()},
		}},
		{"ProtocolCreateRequest", &v1.ProtocolCreateRequest{
			Protocol: &v1.ProtocolCreateRequest_Draft{
				Title:    "Custom Protocol",
				TopicId:  topicID,
				Sections: []*v1.ProtocolSection{protocolSection()},
			},
		}},
		{"ProtocolCreateRequest_Draft", &v1.ProtocolCreateRequest_Draft{
			Title:    "Custom Protocol",
			TopicId:  topicID,
			Sections: []*v1.ProtocolSection{protocolSection()},
		}},
		{"ProtocolEditRequest", &v1.ProtocolEditRequest{
			ProtocolId: protocolID,
			Sections:   []*v1.ProtocolSection{protocolSection()},
		}},
		{"ProtocolTemplateListRequest", &v1.ProtocolTemplateListRequest{}},
		{"ProtocolTemplateList", &v1.ProtocolTemplateList{
			Items: []*v1.ProtocolTemplate{{
				Id:               templateID,
				Title:            "Cochrane-style extraction template",
				ProtocolSections: []*v1.ProtocolSection{protocolSection()},
			}},
			Metadata: listMetadata(),
		}},
		{"ProtocolElementListRequest", &v1.ProtocolElementListRequest{}},
		{"ProtocolElementList", &v1.ProtocolElementList{
			Items:    []*v1.ProtocolElement{protocolElement()},
			Metadata: listMetadata(),
		}},

		// extraction.proto
		{"DataExtractionReview", extractionReview()},
		{"DataExtraction", dataExtraction()},
		{"SourceLocationPage", sourceLocation()[0]},
		{"SourceRect", &v1.SourceRect{X: 72.5, Y: 431.25, W: 268, H: 12.5}},
		{"DataExtractionInput", extractionInput()},
		{"DataExtractionListRequest", &v1.DataExtractionListRequest{PaperId: paperID, ExtractionReviewId: reviewID}},
		{"DataExtractionList", &v1.DataExtractionList{
			Items:    []*v1.DataExtraction{dataExtraction()},
			Metadata: listMetadata(),
		}},
		{"DataExtractionReviewListRequest", &v1.DataExtractionReviewListRequest{
			TopicId: topicID, PaperId: paperID, ProtocolId: protocolID,
		}},
		{"DataExtractionReviewList", &v1.DataExtractionReviewList{
			Items:    []*v1.DataExtractionReview{extractionReview()},
			Metadata: listMetadata(),
		}},
		{"DataExtractionCreateRequest", &v1.DataExtractionCreateRequest{
			TopicId:    topicID,
			PaperId:    paperID,
			ProtocolId: protocolID,
			Data:       []*v1.DataExtractionInput{extractionInput()},
		}},
		{"DataExtractionEditRequest", &v1.DataExtractionEditRequest{
			TopicId:            topicID,
			PaperId:            paperID,
			ProtocolId:         protocolID,
			ExtractionReviewId: reviewID,
			Data:               []*v1.DataExtractionInput{extractionInput()},
		}},
	}
}
