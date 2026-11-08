package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type AIAssistantConfig struct {
	ProjectID             string
	Location              string
	VertexLocation        string
	Model                 string
	DLPInspectTemplate    string
	DLPDeidentifyTemplate string
	ModelArmorTemplate    string
}

type ChatRequest struct {
	Prompt string `json:"prompt"`
}

type ChatResponse struct {
	Response    string `json:"response"`
	DLPRedacted bool   `json:"dlp_redacted"`
}

type TicketStore interface {
	ListSupportTickets(ctx context.Context) ([]string, error)
}

type ModelArmorClient interface {
	SanitizeUserPrompt(ctx context.Context, template, prompt string) (bool, error)
	SanitizeModelResponse(ctx context.Context, template, resp string) (bool, error)
}

type DLPClient interface {
	DeidentifyText(ctx context.Context, inspectTemplate, deidTemplate, text string) (string, error)
}

type LLMClient interface {
	Generate(ctx context.Context, systemPrompt, ticketContext, userPrompt string) (string, error)
}

type AIAssistantServer struct {
	cfg     AIAssistantConfig
	tickets TicketStore
	armor   ModelArmorClient
	dlp     DLPClient
	llm     LLMClient
}

func NewAIAssistantServer(cfg AIAssistantConfig, tickets TicketStore, armor ModelArmorClient, dlp DLPClient, llm LLMClient) *AIAssistantServer {
	if cfg.Location == "" {
		cfg.Location = "europe-west1"
	}
	if cfg.VertexLocation == "" {
		cfg.VertexLocation = "eu"
	}
	if cfg.Model == "" {
		cfg.Model = "gemini-3.5-flash-lite"
	}
	return &AIAssistantServer{
		cfg:     cfg,
		tickets: tickets,
		armor:   armor,
		dlp:     dlp,
		llm:     llm,
	}
}

func (s *AIAssistantServer) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealthz)
	mux.HandleFunc("/api/health", s.handleHealthz)
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/api/chat", s.handleChat)
	return mux
}

func (s *AIAssistantServer) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"service": "ai-assistant",
	})
}

func (s *AIAssistantServer) handleChat(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "method not allowed"})
		return
	}

	var req ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid JSON"})
		return
	}

	// 1. Model Armor Input Screening (fail-closed)
	if s.cfg.ModelArmorTemplate != "" && s.armor != nil {
		blocked, err := s.armor.SanitizeUserPrompt(r.Context(), s.cfg.ModelArmorTemplate, req.Prompt)
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "model armor input screening failed: " + err.Error()})
			return
		}
		if blocked {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error":      "Prompt blocked by Model Armor security policy",
				"blocked_by": "MODEL_ARMOR",
			})
			return
		}
	}

	// 2. Load support tickets from Firestore & optionally de-identify with regional Cloud DLP (fail-closed)
	var ticketList []string
	if s.tickets != nil {
		var err error
		ticketList, err = s.tickets.ListSupportTickets(r.Context())
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "firestore ticket lookup failed: " + err.Error()})
			return
		}
	}
	ticketContext := strings.Join(ticketList, "\n")
	userPrompt := req.Prompt
	dlpRedacted := false

	if s.cfg.DLPDeidentifyTemplate != "" && s.dlp != nil {
		var err error
		ticketContext, err = s.dlp.DeidentifyText(r.Context(), s.cfg.DLPInspectTemplate, s.cfg.DLPDeidentifyTemplate, ticketContext)
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "dlp ticket deidentify failed: " + err.Error()})
			return
		}
		userPrompt, err = s.dlp.DeidentifyText(r.Context(), s.cfg.DLPInspectTemplate, s.cfg.DLPDeidentifyTemplate, userPrompt)
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "dlp prompt deidentify failed: " + err.Error()})
			return
		}
		dlpRedacted = true
	}

	// 3. Call EEA-resident Vertex AI Gemini 3.x (fail-closed)
	systemPrompt := "You are the TelP Secure Shop customer support assistant. Answer customer and support inquiries concisely using the provided Support Ticket Database Context. Always repeat any Reference code provided in the user query verbatim so the support session can be correlated."
	var modelReply string
	if s.llm != nil {
		var err error
		modelReply, err = s.llm.Generate(r.Context(), systemPrompt, ticketContext, userPrompt)
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "vertex ai generation failed: " + err.Error()})
			return
		}
	}

	// 4. Model Armor Output Screening (fail-closed)
	if s.cfg.ModelArmorTemplate != "" && s.armor != nil {
		blocked, err := s.armor.SanitizeModelResponse(r.Context(), s.cfg.ModelArmorTemplate, modelReply)
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "model armor output screening failed: " + err.Error()})
			return
		}
		if blocked {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error":      "Model response blocked by Model Armor security policy",
				"blocked_by": "MODEL_ARMOR",
			})
			return
		}
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(ChatResponse{
		Response:    modelReply,
		DLPRedacted: dlpRedacted,
	})
}

// --- Live GCP REST Implementations ---

func getAccessToken(ctx context.Context) (string, error) {
	if tok := strings.TrimSpace(os.Getenv("CLOUDSDK_AUTH_ACCESS_TOKEN")); tok != "" {
		return tok, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/token", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Metadata-Flavor", "Google")
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("metadata token status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	var payload struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}
	if payload.AccessToken == "" {
		return "", fmt.Errorf("empty access_token from metadata server")
	}
	return payload.AccessToken, nil
}

func resolveMetadataProjectID() string {
	req, err := http.NewRequest(http.MethodGet, "http://metadata.google.internal/computeMetadata/v1/project/project-id", nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Metadata-Flavor", "Google")
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 128))
	return strings.TrimSpace(string(b))
}

func vertexEndpointBaseURL(vertexLocation string) string {
	loc := strings.ToLower(strings.TrimSpace(vertexLocation))
	if loc == "" {
		loc = "eu"
	}
	switch loc {
	case "eu", "us":
		return fmt.Sprintf("https://aiplatform.%s.rep.googleapis.com", loc)
	case "global":
		return "https://aiplatform.googleapis.com"
	default:
		return fmt.Sprintf("https://%s-aiplatform.googleapis.com", loc)
	}
}

type gcpTicketStore struct {
	projectID string
	baseURL   string
	client    *http.Client
}

func (g *gcpTicketStore) ListSupportTickets(ctx context.Context) ([]string, error) {
	if g.projectID == "" {
		return nil, fmt.Errorf("GCP_PROJECT_ID is empty")
	}
	tok, err := getAccessToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("access token: %w", err)
	}
	base := g.baseURL
	if base == "" {
		base = "https://firestore.googleapis.com"
	}
	u := fmt.Sprintf("%s/v1/projects/%s/databases/(default)/documents/support_tickets?pageSize=20",
		strings.TrimRight(base, "/"), url.PathEscape(g.projectID))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("X-Goog-User-Project", g.projectID)
	resp, err := g.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("firestore status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	var docList struct {
		Documents []struct {
			Fields map[string]struct {
				StringValue string `json:"stringValue"`
			} `json:"fields"`
		} `json:"documents"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&docList); err != nil {
		return nil, fmt.Errorf("firestore decode: %w", err)
	}
	out := make([]string, 0, len(docList.Documents))
	for _, d := range docList.Documents {
		if v := d.Fields["content"].StringValue; v != "" {
			out = append(out, v)
		}
	}
	return out, nil
}

type gcpModelArmorClient struct {
	projectID string
	location  string
	baseURL   string
	client    *http.Client
}

func (m *gcpModelArmorClient) callSanitize(ctx context.Context, template, action string, bodyPayload map[string]any) (bool, error) {
	tok, err := getAccessToken(ctx)
	if err != nil {
		return false, fmt.Errorf("access token: %w", err)
	}
	base := m.baseURL
	if base == "" {
		loc := m.location
		if loc == "" {
			loc = "europe-west1"
		}
		base = fmt.Sprintf("https://modelarmor.%s.rep.googleapis.com", url.PathEscape(loc))
	}
	u := fmt.Sprintf("%s/v1/%s:%s", strings.TrimRight(base, "/"), strings.TrimPrefix(template, "/"), action)
	body, err := json.Marshal(bodyPayload)
	if err != nil {
		return false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	if m.projectID != "" {
		req.Header.Set("X-Goog-User-Project", m.projectID)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := m.client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 65536))
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("modelarmor %s status %d: %s", action, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var parsed struct {
		SanitizationResult struct {
			FilterMatchState string `json:"filterMatchState"`
		} `json:"sanitizationResult"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return false, fmt.Errorf("modelarmor decode: %w", err)
	}
	return parsed.SanitizationResult.FilterMatchState == "MATCH_FOUND", nil
}

func (m *gcpModelArmorClient) SanitizeUserPrompt(ctx context.Context, template, prompt string) (bool, error) {
	return m.callSanitize(ctx, template, "sanitizeUserPrompt", map[string]any{
		"userPromptData": map[string]string{"text": prompt},
	})
}

func (m *gcpModelArmorClient) SanitizeModelResponse(ctx context.Context, template, modelResp string) (bool, error) {
	if strings.TrimSpace(modelResp) == "" {
		return false, nil
	}
	return m.callSanitize(ctx, template, "sanitizeModelResponse", map[string]any{
		"modelResponseData": map[string]string{"text": modelResp},
	})
}

type gcpDLPClient struct {
	projectID string
	location  string
	baseURL   string
	client    *http.Client
}

func (d *gcpDLPClient) DeidentifyText(ctx context.Context, inspectTemplate, deidTemplate, text string) (string, error) {
	if strings.TrimSpace(text) == "" {
		return text, nil
	}
	tok, err := getAccessToken(ctx)
	if err != nil {
		return "", fmt.Errorf("access token: %w", err)
	}
	loc := d.location
	if loc == "" {
		loc = "europe-west1"
	}
	base := d.baseURL
	if base == "" {
		base = fmt.Sprintf("https://dlp.%s.rep.googleapis.com", url.PathEscape(loc))
	}
	u := fmt.Sprintf("%s/v2/projects/%s/locations/%s/content:deidentify",
		strings.TrimRight(base, "/"), url.PathEscape(d.projectID), url.PathEscape(loc))
	payload := map[string]any{
		"deidentifyTemplateName": deidTemplate,
		"item":                   map[string]string{"value": text},
	}
	if inspectTemplate != "" {
		payload["inspectTemplateName"] = inspectTemplate
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	if d.projectID != "" {
		req.Header.Set("X-Goog-User-Project", d.projectID)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := d.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 131072))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("dlp status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var res struct {
		Item struct {
			Value string `json:"value"`
		} `json:"item"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return "", fmt.Errorf("dlp decode: %w", err)
	}
	return res.Item.Value, nil
}

type gcpVertexClient struct {
	projectID      string
	vertexLocation string
	model          string
	baseURL        string
	client         *http.Client
}

func (v *gcpVertexClient) Generate(ctx context.Context, systemPrompt, ticketContext, userPrompt string) (string, error) {
	if v.projectID == "" {
		return "", fmt.Errorf("GCP_PROJECT_ID is empty")
	}
	tok, err := getAccessToken(ctx)
	if err != nil {
		return "", fmt.Errorf("access token: %w", err)
	}
	vLoc := strings.TrimSpace(v.vertexLocation)
	if vLoc == "" {
		vLoc = "eu"
	}
	model := strings.TrimSpace(v.model)
	if model == "" {
		model = "gemini-3.5-flash-lite"
	}
	base := v.baseURL
	if base == "" {
		base = vertexEndpointBaseURL(vLoc)
	}
	u := fmt.Sprintf("%s/v1/projects/%s/locations/%s/publishers/google/models/%s:generateContent",
		strings.TrimRight(base, "/"), url.PathEscape(v.projectID), url.PathEscape(vLoc), url.PathEscape(model))

	combinedUser := fmt.Sprintf("Support Ticket Database Context:\n%s\n\nCustomer / Support Agent Query:\n%s", ticketContext, userPrompt)
	reqPayload := map[string]any{
		"systemInstruction": map[string]any{
			"parts": []map[string]any{{"text": systemPrompt}},
		},
		"contents": []map[string]any{
			{
				"role":  "user",
				"parts": []map[string]any{{"text": combinedUser}},
			},
		},
		"generationConfig": map[string]any{
			"temperature":     0.1,
			"maxOutputTokens": 1024,
			"thinkingConfig": map[string]any{
				"thinkingBudget": 0,
			},
		},
	}
	body, err := json.Marshal(reqPayload)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("X-Goog-User-Project", v.projectID)
	req.Header.Set("Content-Type", "application/json")
	resp, err := v.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 262144))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("vertex status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var genResp struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text    string `json:"text"`
					Thought bool   `json:"thought"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(raw, &genResp); err != nil {
		return "", fmt.Errorf("vertex decode: %w", err)
	}
	var textParts []string
	for _, cand := range genResp.Candidates {
		for _, p := range cand.Content.Parts {
			if p.Thought {
				continue
			}
			if strings.TrimSpace(p.Text) != "" {
				textParts = append(textParts, p.Text)
			}
		}
		if len(textParts) > 0 {
			break
		}
	}
	if len(textParts) == 0 {
		return "", fmt.Errorf("vertex returned empty text candidates")
	}
	return strings.TrimSpace(strings.Join(textParts, "\n")), nil
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	projectID := strings.TrimSpace(os.Getenv("GCP_PROJECT_ID"))
	if projectID == "" {
		projectID = strings.TrimSpace(os.Getenv("PROJECT_ID"))
	}
	if projectID == "" {
		projectID = resolveMetadataProjectID()
	}
	loc := strings.TrimSpace(os.Getenv("LOCATION"))
	if loc == "" {
		loc = "europe-west1"
	}
	vertexLoc := strings.TrimSpace(os.Getenv("VERTEX_LOCATION"))
	if vertexLoc == "" {
		vertexLoc = "eu"
	}
	model := strings.TrimSpace(os.Getenv("VERTEX_MODEL"))
	if model == "" {
		model = "gemini-3.5-flash-lite"
	}
	httpClient := &http.Client{Timeout: 20 * time.Second}
	srv := NewAIAssistantServer(
		AIAssistantConfig{
			ProjectID:             projectID,
			Location:              loc,
			VertexLocation:        vertexLoc,
			Model:                 model,
			DLPInspectTemplate:    strings.TrimSpace(os.Getenv("DLP_INSPECT_TEMPLATE")),
			DLPDeidentifyTemplate: strings.TrimSpace(os.Getenv("DLP_DEIDENTIFY_TEMPLATE")),
			ModelArmorTemplate:    strings.TrimSpace(os.Getenv("MODEL_ARMOR_TEMPLATE")),
		},
		&gcpTicketStore{projectID: projectID, client: httpClient},
		&gcpModelArmorClient{projectID: projectID, location: loc, client: httpClient},
		&gcpDLPClient{projectID: projectID, location: loc, client: httpClient},
		&gcpVertexClient{projectID: projectID, vertexLocation: vertexLoc, model: model, client: httpClient},
	)
	log.Printf("TelP Secure Shop ai-assistant listening on :%s (project=%s, location=%s, vertex_location=%s, model=%s)", port, projectID, loc, vertexLoc, model)
	if err := http.ListenAndServe(":"+port, srv.Routes()); err != nil {
		log.Fatal(err)
	}
}
