package api

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stellarlinkco/agentsdk-go/pkg/model"
	"github.com/stellarlinkco/agentsdk-go/pkg/tool"
)

// deltaStreamModel emits real provider-shaped deltas before Final for CompleteStream.
type deltaStreamModel struct {
	deltas []string
	final  *model.Response
}

func (m *deltaStreamModel) Complete(ctx context.Context, req model.Request) (*model.Response, error) {
	if m.final != nil {
		return m.final, nil
	}
	return &model.Response{Message: model.Message{Role: "assistant"}}, nil
}

func (m *deltaStreamModel) CompleteStream(ctx context.Context, req model.Request, cb model.StreamHandler) error {
	for _, d := range m.deltas {
		if err := cb(model.StreamResult{Delta: d}); err != nil {
			return err
		}
	}
	fr := m.final
	if fr == nil {
		fr = &model.Response{Message: model.Message{Role: "assistant", Content: strings.Join(m.deltas, "")}}
	}
	return cb(model.StreamResult{Final: true, Response: fr})
}

func TestRunStream_LiveTextDeltasNoSyntheticReplay(t *testing.T) {
	root := newClaudeProject(t)
	mdl := &deltaStreamModel{
		deltas: []string{"hel", "lo"},
		final: &model.Response{
			Message: model.Message{Role: "assistant", Content: "hello"},
		},
	}
	rt, err := New(context.Background(), Options{ProjectRoot: root, Model: mdl})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })
	stream, err := rt.RunStream(context.Background(), Request{Prompt: "ping"})
	if err != nil {
		t.Fatalf("RunStream: %v", err)
	}
	var types []string
	var texts []string
	for evt := range stream {
		switch evt.Type {
		case EventContentBlockStart, EventContentBlockStop:
			types = append(types, evt.Type)
		case EventContentBlockDelta:
			if evt.Delta != nil && evt.Delta.Type == "text_delta" {
				types = append(types, evt.Type)
				texts = append(texts, evt.Delta.Text)
			}
		}
	}
	wantPrefix := []string{EventContentBlockStart, EventContentBlockDelta, EventContentBlockDelta, EventContentBlockStop}
	if len(types) < len(wantPrefix) {
		t.Fatalf("events = %v, want prefix %v", types, wantPrefix)
	}
	for i := range wantPrefix {
		if types[i] != wantPrefix[i] {
			t.Fatalf("at %d: got %s, want %s (full %v)", i, types[i], wantPrefix[i], types)
		}
	}
	if len(texts) != 2 || texts[0] != "hel" || texts[1] != "lo" {
		t.Fatalf("delta texts = %q, want [hel lo]", texts)
	}
	for _, d := range texts {
		if d == "" {
			t.Fatal("empty delta")
		}
	}
	if strings.Join(texts, "") != "hello" {
		t.Fatalf("joined = %q", strings.Join(texts, ""))
	}
}

func TestRunStream_LiveTextThenToolUsesIndexOneForToolBlock(t *testing.T) {
	root := newClaudeProject(t)
	echo := &echoTool{}
	first := &model.Response{
		Message: model.Message{
			Role:    "assistant",
			Content: "calltool",
			ToolCalls: []model.ToolCall{{
				ID:        "call_1",
				Name:      "echo",
				Arguments: map[string]any{"text": "hi"},
			}},
		},
	}
	second := &model.Response{
		Message: model.Message{Role: "assistant", Content: "ok"},
	}
	mdl := &deltaStreamTwoRoundModel{
		firstDeltas: []string{"call", "tool"},
		first:       first,
		second:      second,
	}
	rt, err := New(context.Background(), Options{ProjectRoot: root, Model: mdl, Tools: []tool.Tool{echo}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })
	stream, err := rt.RunStream(context.Background(), Request{Prompt: "go"})
	if err != nil {
		t.Fatalf("RunStream: %v", err)
	}
	var toolBlockIdx []int
	var sawLiveTextStop bool
	for evt := range stream {
		if evt.Type == EventContentBlockStop {
			sawLiveTextStop = true
		}
		if evt.Type == EventContentBlockStart && evt.ContentBlock != nil && evt.ContentBlock.Type == "tool_use" {
			if evt.Index != nil {
				toolBlockIdx = append(toolBlockIdx, *evt.Index)
			}
		}
	}
	if !sawLiveTextStop {
		t.Fatal("expected content_block_stop after live text")
	}
	if len(toolBlockIdx) < 1 {
		t.Fatalf("expected tool_use content_block_start, got idx list %v", toolBlockIdx)
	}
	if toolBlockIdx[0] != 1 {
		t.Fatalf("first tool block index = %d, want 1 (after live text at 0)", toolBlockIdx[0])
	}
}

// deltaStreamTwoRoundModel: first CompleteStream emits deltas + tool turn; second round Final only.
type deltaStreamTwoRoundModel struct {
	firstDeltas []string
	first       *model.Response
	second      *model.Response
	round       int
}

func (m *deltaStreamTwoRoundModel) Complete(ctx context.Context, req model.Request) (*model.Response, error) {
	if m.round == 0 {
		return m.first, nil
	}
	return m.second, nil
}

func (m *deltaStreamTwoRoundModel) CompleteStream(ctx context.Context, req model.Request, cb model.StreamHandler) error {
	if m.round == 0 {
		for _, d := range m.firstDeltas {
			if err := cb(model.StreamResult{Delta: d}); err != nil {
				return err
			}
		}
		if err := cb(model.StreamResult{Final: true, Response: m.first}); err != nil {
			return err
		}
		m.round++
		return nil
	}
	return cb(model.StreamResult{Final: true, Response: m.second})
}

func TestRun_IgnoresLiveForwardNoDuplicateFinal(t *testing.T) {
	root := newClaudeProject(t)
	mdl := &deltaStreamModel{
		deltas: []string{"a", "b"},
		final: &model.Response{
			Message: model.Message{Role: "assistant", Content: "ab"},
		},
	}
	rt, err := New(context.Background(), Options{ProjectRoot: root, Model: mdl})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })
	resp, err := rt.Run(context.Background(), Request{Prompt: "ping"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if resp == nil || resp.Result == nil {
		t.Fatal("nil response")
	}
	if resp.Result.Output != "ab" {
		t.Fatalf("output = %q, want ab", resp.Result.Output)
	}
}

type deltaThenStreamErrModel struct{}

func (deltaThenStreamErrModel) Complete(context.Context, model.Request) (*model.Response, error) {
	return nil, errors.New("complete not used")
}

func (deltaThenStreamErrModel) CompleteStream(_ context.Context, _ model.Request, cb model.StreamHandler) error {
	if err := cb(model.StreamResult{Delta: "x"}); err != nil {
		return err
	}
	return errors.New("stream aborted before final")
}

func TestRunStream_LiveTextBlockClosedOnStreamError(t *testing.T) {
	root := newClaudeProject(t)
	rt, err := New(context.Background(), Options{ProjectRoot: root, Model: deltaThenStreamErrModel{}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })
	stream, err := rt.RunStream(context.Background(), Request{Prompt: "ping"})
	if err != nil {
		t.Fatalf("RunStream: %v", err)
	}
	var types []string
	for evt := range stream {
		types = append(types, evt.Type)
	}
	gotStop := false
	for i, typ := range types {
		if typ == EventContentBlockStop {
			gotStop = true
			if i == 0 || types[i-1] != EventContentBlockDelta {
				t.Fatalf("stop should follow text delta, got seq %v", types)
			}
			break
		}
	}
	if !gotStop {
		t.Fatalf("expected content_block_stop after stream error, got %v", types)
	}
}
