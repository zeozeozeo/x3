package systemone

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testClient(t *testing.T, handler http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := NewClient(Config{BaseURL: server.URL, Token: "secret", Model: ModelFlash, Timeout: 2 * time.Second})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return client, server
}

func TestQuestionMarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		question Question
		want     string
	}{
		{
			name:     "noul without criteria",
			question: Noul("Should the bot reply?", NoulCriteria{}),
			want:     `{"instructions":"Should the bot reply?","type":"noul"}`,
		},
		{
			name:     "noul with criteria",
			question: Noul("Should the bot reply?", NoulCriteria{True: "asked", False: "chatter"}),
			want:     `{"criteria":{"true":"asked","false":"chatter"},"instructions":"Should the bot reply?","type":"noul"}`,
		},
		{
			name:     "choice uses object criteria",
			question: Choice("Which team?", map[string]string{"billing": "Invoices"}),
			want:     `{"criteria":{"billing":"Invoices"},"instructions":"Which team?","type":"choice"}`,
		},
		{
			name:     "score uses array criteria",
			question: Score("How bad?", []string{"fine", "broken"}),
			want:     `{"criteria":["fine","broken"],"instructions":"How bad?","type":"score"}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			encoded, err := json.Marshal(tc.question)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if string(encoded) != tc.want {
				t.Fatalf("got %s want %s", encoded, tc.want)
			}
		})
	}
}

func TestQuestionMarshalJSONUnknownType(t *testing.T) {
	if _, err := json.Marshal(Question{Type: "guess", Instructions: "?"}); err == nil {
		t.Fatal("expected error for unknown question type")
	}
}

func TestDecideSendsRequestAndParsesAnswers(t *testing.T) {
	var gotPath, gotAuth, gotContentType string
	var gotBody Request
	client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &gotBody); err != nil {
			t.Errorf("decode request: %v", err)
		}
		io.WriteString(w, `{"result":{"model":"clef-flash","answers":{
			"respond":{"type":"noul","noul":0.93},
			"addressed":{"type":"choice","choice":"direct","probabilities":{"direct":0.9,"ambient":0.1},"confidence":0.9},
			"invitation":{"type":"score","score":2.4,"legend":{"0":"silent","1":"mild","2":"strong"},"probabilities":{"0":0.05,"1":0.15,"2":0.8},"confidence":0.7}
		},"usage":{"input_tokens":120,"output_tokens":8}},"success":true,"errors":[],"messages":[]}`)
	})

	result, err := client.Decide(context.Background(), Request{
		State:     "conversation transcript",
		Questions: map[string]Question{"respond": Noul("Reply?", NoulCriteria{})},
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}

	if gotPath != "/" {
		t.Errorf("path = %q", gotPath)
	}
	if gotAuth != "Bearer secret" {
		t.Errorf("auth = %q", gotAuth)
	}
	if gotContentType != "application/json" {
		t.Errorf("content type = %q", gotContentType)
	}
	if gotBody.Model != ModelFlash {
		t.Errorf("request model = %q, want %q", gotBody.Model, ModelFlash)
	}
	if len(gotBody.Questions) != 1 {
		t.Errorf("request questions = %d", len(gotBody.Questions))
	}

	if got, ok := result.Noul("respond"); !ok || got != 0.93 {
		t.Errorf("respond noul = %v (%v)", got, ok)
	}
	if got, ok := result.Selected("addressed"); !ok || got != "direct" {
		t.Errorf("addressed choice = %q (%v)", got, ok)
	}
	invitation := result.Answers["invitation"]
	if invitation.Score == nil || *invitation.Score != 2.4 {
		t.Errorf("invitation score = %v", invitation.Score)
	}
	if invitation.Certainty() != 0.7 {
		t.Errorf("invitation certainty = %v", invitation.Certainty())
	}
	if len(invitation.Probabilities) != 3 {
		t.Errorf("invitation probabilities = %v", invitation.Probabilities)
	}
	if invitation.Legend["2"] != "strong" {
		t.Errorf("invitation legend = %v", invitation.Legend)
	}
	if result.Usage.InputTokens != 120 || result.Usage.OutputTokens != 8 {
		t.Errorf("usage = %+v", result.Usage)
	}
	if result.Model != ModelFlash {
		t.Errorf("result model = %q", result.Model)
	}
}

func TestDecideParsesBareSpecResponse(t *testing.T) {
	client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"model":"typesafe-jev-1.13.0","answers":{"respond":{"type":"noul","noul":0.1}},"usage":{"input_tokens":10,"output_tokens":2}}`)
	})
	result, err := client.Decide(context.Background(), Request{
		Questions: map[string]Question{"respond": Noul("Reply?", NoulCriteria{})},
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if result.Model != "typesafe-jev-1.13.0" {
		t.Errorf("model = %q", result.Model)
	}
	if got, _ := result.Noul("respond"); got != 0.1 {
		t.Errorf("noul = %v", got)
	}
}

func TestDecideDefaultsModelToClientModel(t *testing.T) {
	var gotModel string
	client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var decoded struct {
			Model string `json:"model"`
		}
		json.Unmarshal(body, &decoded)
		gotModel = decoded.Model
		io.WriteString(w, `{"answers":{"respond":{"type":"noul","noul":0.5}}}`)
	})
	if _, err := client.Decide(context.Background(), Request{
		Questions: map[string]Question{"respond": Noul("Reply?", NoulCriteria{})},
	}); err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if gotModel != ModelFlash {
		t.Errorf("model = %q", gotModel)
	}
}

func TestDecideRequiresQuestions(t *testing.T) {
	client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("request should not have been sent")
	})
	if _, err := client.Decide(context.Background(), Request{State: "hi"}); err == nil {
		t.Fatal("expected error for empty questions")
	}
}

func TestDecideErrors(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		wantSub string
	}{
		{
			name:    "unauthorized",
			status:  http.StatusUnauthorized,
			body:    `{"success":false,"errors":[{"code":10000,"message":"Authentication error"}]}`,
			wantSub: "Authentication error",
		},
		{
			name:    "rate limited",
			status:  http.StatusTooManyRequests,
			body:    `{"success":false,"errors":[{"code":7000,"message":"Rate limited"}]}`,
			wantSub: "Rate limited",
		},
		{
			name:    "malformed json",
			status:  http.StatusOK,
			body:    `{"result":`,
			wantSub: "decode response",
		},
		{
			name:    "no answers",
			status:  http.StatusOK,
			body:    `{"result":{"model":"clef-flash","answers":{}},"success":true}`,
			wantSub: "no answers",
		},
		{
			name:    "empty body",
			status:  http.StatusOK,
			body:    ``,
			wantSub: "decode response",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				io.WriteString(w, tc.body)
			})
			_, err := client.Decide(context.Background(), Request{
				Questions: map[string]Question{"respond": Noul("Reply?", NoulCriteria{})},
			})
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("error = %v, want substring %q", err, tc.wantSub)
			}
		})
	}
}

func TestDecideHonorsContextCancellation(t *testing.T) {
	client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := client.Decide(ctx, Request{
		Questions: map[string]Question{"respond": Noul("Reply?", NoulCriteria{})},
	})
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestNewClientValidation(t *testing.T) {
	if _, err := NewClient(Config{}); err == nil {
		t.Fatal("expected error for missing base url")
	}
	client, err := NewClient(Config{BaseURL: "https://example.test/run"})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if client.model != ModelFlash {
		t.Errorf("default model = %q", client.model)
	}
	if client.timeout != defaultTimeout {
		t.Errorf("default timeout = %v", client.timeout)
	}
}

func TestDecideSendsImagesExtension(t *testing.T) {
	var gotBody Request
	client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &gotBody)
		io.WriteString(w, `{"answers":{"respond":{"type":"noul","noul":0.7}}}`)
	})
	if _, err := client.Decide(context.Background(), Request{
		Questions: map[string]Question{"respond": Noul("Reply?", NoulCriteria{})},
		Images:    []Image{{ContentType: "image/png", Base64: "aGk="}},
	}); err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if len(gotBody.Images) != 1 || gotBody.Images[0].ContentType != "image/png" {
		t.Fatalf("images = %+v", gotBody.Images)
	}
}
