package contract_test

import (
	"time"

	v1 "github.com/metacensus/ui/contract/go/metacensus/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	"google.golang.org/protobuf/types/known/wrapperspb"
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
	topicID    = "topc:0192a642-817d-7a3e-a282-d7a282ebd482"
	userID     = "user:0192a642-817d-7a3e-a282-d7a282ebd483"
	reviewerID = "user:0192a642-817d-7a3e-a282-d7a282ebd484"
	propID     = "prop:0192a642-817d-7a3e-a282-d7a282ebd485"

	// demo's serials, stringified. Papers, protocols and extractions exist only
	// in demo, so these are the ids a caller sees today.
	paperID           = "17"
	protocolID        = "4"
	sectionID         = "9"
	elementID         = "31"
	extractionID      = "204"
	reviewID          = "58"
	domainID          = "2"
	categoryID        = "6"
	templateID        = "3"
	voteID            = "91"
	groupID           = "5"
	approvalCreatedAt = "2024-11-02T09:14:00Z"
)

func ts(s string) *timestamppb.Timestamp {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return timestamppb.New(t)
}

func userReference() *v1.UserReference {
	return &v1.UserReference{Id: userID, Name: "Ada Okonkwo", Role: v1.User_Contributor}
}

func protocolElement() *v1.ProtocolElement {
	return &v1.ProtocolElement{
		Id:                 elementID,
		Name:               "studyDesign",
		Type:               "radio",
		Label:              "Study design",
		Placeholder:        "",
		Required:           true,
		ConditionallyShown: false,
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
		CreatedAt:        ts("2024-10-20T08:00:00Z"),
		UpdatedAt:        ts("2024-11-01T16:30:00Z"),
	}
}

func user() *v1.User {
	return &v1.User{
		Id:        userID,
		Name:      "Ada Okonkwo",
		Email:     "ada@example.org",
		Country:   "Nigeria",
		Role:      v1.User_Contributor,
		JobTitle:  "Clinical pharmacologist",
		Bio:       "Works on pharmacogenomics of fluoropyrimidine toxicity.",
		CreatedAt: ts("2024-09-01T10:00:00Z"),
		UpdatedAt: ts("2024-11-12T11:45:00Z"),
		// infra-only accounting; a demo-backed deployment leaves these unset,
		// which is why the golden for a demo-shaped user would omit lastActive.
		LastActive:  ts("2024-11-12T11:45:00Z"),
		LastCredits: 18446744073709551615, // max uint64: protojson must quote it
		Groups: []*v1.GroupMembership{
			{Id: groupID, Name: "Oncology working group", Role: "group admin"},
		},
		TopicContributors: []*v1.UserTopicMembership{
			{Topic: &v1.Reference{Id: topicID, Name: "DPYD Genotype and Fluoropyrimidine Toxicity"}},
		},
	}
}

func topic() *v1.Topic {
	return &v1.Topic{
		Id:                       topicID,
		Created:                  ts("2024-10-19T13:56:05Z"),
		Name:                     "DPYD Genotype and Fluoropyrimidine Toxicity",
		Description:              "Association of DPYD genotype to fluoropyrimidine toxicity.",
		Question:                 "Does DPYD genotype-guided dosing reduce severe toxicity?",
		Status:                   v1.Topic_ReviewingPapers,
		StatusDescription:        "Screening the 2019-2024 window.",
		MinimumExtractionReviews: 2,
		Domain:                   &v1.Reference{Id: domainID, Name: "Oncology"},
		TopicCategories: []*v1.TopicCategory{
			{Category: &v1.Reference{Id: categoryID, Name: "Pharmacogenomics"}},
		},
		TopicReviewers: []*v1.TopicReviewer{
			// `role` is Unspecified because demo's reviewer projection does not
			// select it. It is still emitted, because enums always are.
			{Reviewer: &v1.UserReference{Id: reviewerID, Name: "Bo Lindqvist"}},
		},
		TopicAdmins:       []*v1.TopicAdmin{{Admin: &v1.UserReference{Id: userID, Name: "Ada Okonkwo", Role: v1.User_Admin}}},
		TopicContributors: []*v1.TopicContributor{{Contributor: userReference()}},
		Protocol:          protocol(),
	}
}

func vote() *v1.Vote {
	return &v1.Vote{
		Id:          voteID,
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
		TopicId:     topicID,
		AuthorId:    userID,
		Created:     ts("2024-11-05T14:02:00Z"),
		Type:        v1.Prop_Statement,
		Description: "DPYD genotype-guided dosing reduces grade 3+ toxicity.",
		// `concluded` is left unset: the prop is still open. That absence is
		// the whole reason it is a Timestamp rather than the empty string demo
		// emits today, and the golden proves the field simply does not appear.
		Conclusion: v1.PropConclusion_Open,
		Votes:      []*v1.Vote{vote()},
	}
}

func paper() *v1.Paper {
	return &v1.Paper{
		Id:        paperID,
		Title:     "DPYD genotype-guided dose individualisation of fluoropyrimidine therapy",
		Authors:   []string{"Linda M. Henricks", "Carin A. T. C. Lunenburg"},
		Abstract:  "BACKGROUND: Fluoropyrimidines are widely used...",
		Status:    v1.Paper_PendingReview,
		TopicId:   topicID,
		Domain:    "Oncology",
		Doi:       "10.1016/S1470-2045(18)30686-7",
		Pmid:      "30348537",
		CreatedAt: ts("2024-10-22T12:00:00Z"),
		UpdatedAt: ts("2024-10-22T12:00:00Z"),
		Url:       "https://s3.example.org/papers/17/henricks-2018.pdf?X-Amz-Expires=900",
		S3Key:     "papers/17/henricks-2018.pdf",

		FullTextUrl:       "https://pubmed.ncbi.nlm.nih.gov/30348537/",
		Description:       "Prospective multicentre safety analysis.",
		PaperContributors: []*v1.PaperContributor{{Contributor: userReference()}},
		PaperApprovalsRejections: []*v1.PaperApprovalRejection{
			{Approval: true, CreatedAt: ts(approvalCreatedAt), User: userReference()},
		},
	}
}

func extractionReview() *v1.DataExtractionReview {
	return &v1.DataExtractionReview{
		Id:         reviewID,
		PaperId:    paperID,
		ProtocolId: protocolID,
		TopicId:    topicID,
		// `userId` is left empty rather than omitted: it is a plain string, so
		// EmitDefaultValues emits `""`. That is the honest rendering of demo
		// never setting the column.
		UserId:    "",
		CreatedAt: ts("2024-11-08T09:00:00Z"),
		UpdatedAt: ts("2024-11-08T09:41:00Z"),
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
		PaperId:                paperID,
		ProtocolId:             protocolID,
		ProtocolElementId:      elementID,
		UserId:                 "",
		DataExtractionReviewId: reviewID,
		Data:                   "rct",
		SourceLocation:         sourceLocation(),
		CreatedAt:              ts("2024-11-08T09:12:00Z"),
		UpdatedAt:              ts("2024-11-08T09:12:00Z"),
	}
}

func listMetadata() *v1.ListMetadata {
	return &v1.ListMetadata{Page: 1, Limit: 30, Total: wrapperspb.Int32(1)}
}

// listMetadataNoTotal is the metadata every backend actually produces today:
// nobody computes a total, so the field is absent rather than zero.
func listMetadataNoTotal() *v1.ListMetadata {
	return &v1.ListMetadata{Page: 1, Limit: 30}
}

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
		{"ListMetadataWithoutTotal", listMetadataNoTotal()},
		{"Reference", &v1.Reference{Id: domainID, Name: "Oncology"}},
		{"Error", &v1.Error{Error: "Invalid credentials"}},
		{"HealthcheckResponse", &v1.HealthcheckResponse{Status: "healthy"}},

		// user.proto
		{"User", user()},
		{"GroupMembership", &v1.GroupMembership{Id: groupID, Name: "Oncology working group", Role: "group admin"}},
		{"UserTopicMembership", &v1.UserTopicMembership{Topic: &v1.Reference{Id: topicID, Name: "DPYD Genotype and Fluoropyrimidine Toxicity"}}},
		{"UserReference", userReference()},
		{"LoginRequest", &v1.LoginRequest{Email: "ada@example.org", Password: "correct horse battery staple"}},
		{"SignUpRequest", &v1.SignUpRequest{Name: "Ada Okonkwo", Email: "ada@example.org", Country: "Nigeria", Password: "correct horse battery staple"}},
		{"Session", &v1.Session{Token: "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.e30.signature", User: user()}},
		{"LogoutResponse", &v1.LogoutResponse{}},
		{"UserListRequest", &v1.UserListRequest{Role: v1.User_Admin, Page: 1, Limit: 100}},
		{"UserList", &v1.UserList{Items: []*v1.User{user()}, Metadata: listMetadata()}},
		{"UserGetRequest", &v1.UserGetRequest{UserId: userID}},

		// taxonomy.proto
		{"Domain", &v1.Domain{Id: domainID, Name: "Oncology"}},
		{"Category", &v1.Category{Id: categoryID, Name: "Pharmacogenomics"}},
		{"DomainListRequest", &v1.DomainListRequest{}},
		{"DomainList", &v1.DomainList{Items: []*v1.Domain{{Id: domainID, Name: "Oncology"}}, Metadata: listMetadataNoTotal()}},
		{"CategoryListRequest", &v1.CategoryListRequest{}},
		{"CategoryList", &v1.CategoryList{Items: []*v1.Category{{Id: categoryID, Name: "Pharmacogenomics"}}, Metadata: listMetadataNoTotal()}},

		// topic.proto
		{"Topic", topic()},
		{"TopicCategory", &v1.TopicCategory{Category: &v1.Reference{Id: categoryID, Name: "Pharmacogenomics"}}},
		{"TopicReviewer", &v1.TopicReviewer{Reviewer: &v1.UserReference{Id: reviewerID, Name: "Bo Lindqvist"}}},
		{"TopicAdmin", &v1.TopicAdmin{Admin: &v1.UserReference{Id: userID, Name: "Ada Okonkwo", Role: v1.User_Admin}}},
		{"TopicContributor", &v1.TopicContributor{Contributor: userReference()}},
		{"TopicListRequest", &v1.TopicListRequest{Page: 1, Limit: 50}},
		{"TopicList", &v1.TopicList{Items: []*v1.Topic{topic()}, Metadata: listMetadata()}},
		{"TopicGetRequest", &v1.TopicGetRequest{TopicId: topicID}},
		{"TopicCreateRequest", &v1.TopicCreateRequest{
			Id:                       "",
			Name:                     "DPYD Genotype and Fluoropyrimidine Toxicity",
			Description:              "Association of DPYD genotype to fluoropyrimidine toxicity.",
			Question:                 "Does DPYD genotype-guided dosing reduce severe toxicity?",
			MinimumExtractionReviews: 2,
			Domain:                   &v1.Reference{Id: domainID, Name: "Oncology"},
			Categories:               []*v1.Reference{{Id: categoryID, Name: "Pharmacogenomics"}},
			Admins:                   []*v1.Reference{{Id: userID, Name: "Ada Okonkwo"}},
		}},
		{"TopicProtocolListRequest", &v1.TopicProtocolListRequest{TopicId: topicID}},
		{"TopicProtocolList", &v1.TopicProtocolList{Items: []*v1.Topic{topic()}, Metadata: listMetadataNoTotal()}},
		{"TopicMyVoteListRequest", &v1.TopicMyVoteListRequest{TopicId: topicID}},
		{"TopicMyVoteList", &v1.TopicMyVoteList{Items: []string{propID}, Metadata: listMetadataNoTotal()}},
		{"Member", &v1.Member{
			Id:          userID,
			Created:     ts("2024-10-19T14:00:00Z"),
			LastActive:  ts("2024-11-12T11:45:00Z"),
			LastCredits: 120,
		}},
		{"MemberListRequest", &v1.MemberListRequest{TopicId: topicID}},
		{"MemberList", &v1.MemberList{
			Items:    []*v1.Member{{Id: userID, Created: ts("2024-10-19T14:00:00Z"), LastActive: ts("2024-11-12T11:45:00Z"), LastCredits: 120}},
			Metadata: listMetadataNoTotal(),
		}},
		{"MemberGetRequest", &v1.MemberGetRequest{TopicId: topicID, UserId: userID}},

		// prop.proto
		{"Prop", prop()},
		{"PropConclusion", &v1.PropConclusion{}},
		{"Vote", vote()},
		{"PropCitation", &v1.PropCitation{Start: 12, End: 48}},
		{"PropListRequest", &v1.PropListRequest{TopicId: topicID}},
		{"PropList", &v1.PropList{Items: []*v1.Prop{prop()}, Metadata: listMetadataNoTotal()}},
		{"PropGetRequest", &v1.PropGetRequest{TopicId: topicID, PropId: propID}},
		{"PropCreateRequest", &v1.PropCreateRequest{
			Type:        v1.Prop_TopicQuestion,
			Description: "Does DPYD genotype-guided dosing reduce severe toxicity?",
		}},
		{"VoteListRequest", &v1.VoteListRequest{TopicId: topicID, PropId: propID}},
		{"VoteList", &v1.VoteList{Items: []*v1.Vote{vote()}, Metadata: listMetadataNoTotal()}},
		{"VoteSetRequest", &v1.VoteSetRequest{
			Position:    v1.Vote_Against,
			Explanation: "The cited cohort excludes DPYD*2A heterozygotes.",
			Citations:   []*v1.PropCitation{{Start: 12, End: 48}},
		}},

		// paper.proto
		{"Paper", paper()},
		{"PaperAuthor", &v1.PaperAuthor{Name: "Linda M. Henricks"}},
		{"PaperContributor", &v1.PaperContributor{Contributor: userReference()}},
		{"PaperApprovalRejection", &v1.PaperApprovalRejection{Approval: false, CreatedAt: ts(approvalCreatedAt), User: userReference()}},
		{"PaperListRequest", &v1.PaperListRequest{TopicId: topicID, PaperId: "", Page: 1, Limit: 30}},
		{"PaperList", &v1.PaperList{Items: []*v1.Paper{paper()}, Metadata: listMetadata()}},
		{"PaperCreateRequest", &v1.PaperCreateRequest{
			TopicId:     topicID,
			Title:       "DPYD genotype-guided dose individualisation of fluoropyrimidine therapy",
			Authors:     []*v1.PaperAuthor{{Name: "Linda M. Henricks"}, {Name: "Carin A. T. C. Lunenburg"}},
			Abstract:    "BACKGROUND: Fluoropyrimidines are widely used...",
			Doi:         "10.1016/S1470-2045(18)30686-7",
			Pmid:        "30348537",
			FullTextUrl: "https://pubmed.ncbi.nlm.nih.gov/30348537/",
			Description: "Prospective multicentre safety analysis.",
			Url:         "",
		}},
		{"PaperLookupRequest", &v1.PaperLookupRequest{Pmid: "30348537"}},
		{"PaperLookupResponse", &v1.PaperLookupResponse{
			Pmid:          "30348537",
			Title:         "DPYD genotype-guided dose individualisation of fluoropyrimidine therapy",
			Authors:       []string{"Linda M. Henricks", "Carin A. T. C. Lunenburg"},
			Abstract:      "BACKGROUND: Fluoropyrimidines are widely used...",
			Doi:           "10.1016/S1470-2045(18)30686-7",
			PublishedDate: "2018-11-01",
			Url:           "https://doi.org/10.1016/S1470-2045(18)30686-7",
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
		{"ProtocolListRequest", &v1.ProtocolListRequest{Id: topicID, Page: 1, Limit: 10}},
		{"ProtocolTemplateListRequest", &v1.ProtocolTemplateListRequest{Id: "", Page: 1, Limit: 10}},
		{"ProtocolTemplateList", &v1.ProtocolTemplateList{
			Items: []*v1.ProtocolTemplate{{
				Id:               templateID,
				Title:            "Cochrane-style extraction template",
				ProtocolSections: []*v1.ProtocolSection{protocolSection()},
			}},
			Metadata: listMetadataNoTotal(),
		}},
		{"ProtocolElementListRequest", &v1.ProtocolElementListRequest{}},
		{"ProtocolElementList", &v1.ProtocolElementList{
			Items:    []*v1.ProtocolElement{protocolElement()},
			Metadata: listMetadataNoTotal(),
		}},

		// extraction.proto
		{"DataExtractionReview", extractionReview()},
		{"DataExtraction", dataExtraction()},
		{"SourceLocationPage", sourceLocation()[0]},
		{"SourceRect", &v1.SourceRect{X: 72.5, Y: 431.25, W: 268, H: 12.5}},
		{"DataExtractionInput", &v1.DataExtractionInput{
			ProtocolElementId: elementID,
			ProtocolId:        protocolID,
			PaperId:           paperID,
			Data:              "rct",
			SourceLocation:    sourceLocation(),
		}},
		{"DataExtractionListRequest", &v1.DataExtractionListRequest{PaperId: paperID, ExtractionReviewId: reviewID}},
		{"DataExtractionList", &v1.DataExtractionList{
			Items:    []*v1.DataExtraction{dataExtraction()},
			Metadata: listMetadataNoTotal(),
		}},
		{"DataExtractionReviewListRequest", &v1.DataExtractionReviewListRequest{
			TopicId: topicID, PaperId: paperID, ProtocolId: protocolID, Page: 1, Limit: 10,
		}},
		{"DataExtractionReviewList", &v1.DataExtractionReviewList{
			Items:    []*v1.DataExtractionReview{extractionReview()},
			Metadata: listMetadataNoTotal(),
		}},
		{"DataExtractionCreateRequest", &v1.DataExtractionCreateRequest{
			TopicId:    topicID,
			PaperId:    paperID,
			ProtocolId: protocolID,
			Data: []*v1.DataExtractionInput{{
				ProtocolElementId: elementID,
				ProtocolId:        protocolID,
				PaperId:           paperID,
				Data:              "rct",
				SourceLocation:    sourceLocation(),
			}},
		}},
		{"DataExtractionEditRequest", &v1.DataExtractionEditRequest{
			TopicId:            topicID,
			PaperId:            paperID,
			ProtocolId:         protocolID,
			ExtractionReviewId: reviewID,
			Data: []*v1.DataExtractionInput{{
				ProtocolElementId: elementID,
				ProtocolId:        protocolID,
				PaperId:           paperID,
				Data:              "rct",
				SourceLocation:    sourceLocation(),
			}},
		}},

		// lit_search.proto
		{"LitSearchAbstractRequest", &v1.LitSearchAbstractRequest{Id: "30348537", Db: "pubmed"}},
		{"LitSearchAbstract", &v1.LitSearchAbstract{Paragraphs: []string{
			"BACKGROUND: Fluoropyrimidines are widely used...",
			"METHODS: Patients were prospectively genotyped...",
		}}},
	}
}
