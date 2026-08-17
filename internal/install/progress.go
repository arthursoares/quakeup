package install

import (
	"regexp"
	"strconv"
	"strings"
)

// steamParser incrementally consumes raw PTY output from SteamCMD and turns
// it into actions: progress updates, interactive prompts, log lines, and
// success/failure markers. SteamCMD writes prompts without a trailing
// newline, so the unterminated tail of the stream must be inspected too.
type steamParser struct {
	tail      string
	installed map[string]bool
	failure   string
	lastLine  string
	prompted  map[string]bool
}

type actionKind int

const (
	actLog actionKind = iota
	actProgress
	actPrompt
)

type action struct {
	kind   actionKind
	line   string
	verb   string
	done   int64
	total  int64
	masked bool
}

var (
	progressRe = regexp.MustCompile(`Update state \(0x[0-9a-fA-F]+\) ([a-z ]+), progress: [0-9.]+ \((\d+) / (\d+)\)`)
	successRe  = regexp.MustCompile(`Success! App '(\d+)' fully installed`)
	failureRe  = regexp.MustCompile(`(?:ERROR! (.+)|^\s*FAILED \((.+)\)|Login Failure: (.+))`)
	// Prompts SteamCMD emits without a newline. "password:" is matched
	// case-insensitively at the end of the pending output; Steam Guard used
	// several phrasings across SteamCMD versions.
	promptRe = regexp.MustCompile(`(?i)(password|two-factor code|steam guard code|auth code|code from your .*app)\s*:\s*$`)
)

func newSteamParser() *steamParser {
	return &steamParser{installed: map[string]bool{}, prompted: map[string]bool{}}
}

func (p *steamParser) sawSuccess(appID string) bool { return p.installed[appID] }

func (p *steamParser) feed(chunk string) []action {
	var acts []action
	p.tail += chunk
	// Normalize: SteamCMD uses \r both in CRLF pairs and for in-place
	// progress updates; treat both as line terminators.
	p.tail = strings.ReplaceAll(p.tail, "\r\n", "\n")
	p.tail = strings.ReplaceAll(p.tail, "\r", "\n")
	for {
		i := strings.IndexByte(p.tail, '\n')
		if i < 0 {
			break
		}
		line := strings.TrimSpace(p.tail[:i])
		p.tail = p.tail[i+1:]
		if line == "" {
			continue
		}
		acts = append(acts, p.parseLine(line)...)
	}
	// A prompt sits in the unterminated tail. Deduplicate: the answer we
	// write is echoed back by the PTY, which would otherwise re-trigger it.
	if m := promptRe.FindString(p.tail); m != "" {
		key := strings.ToLower(m)
		if !p.prompted[key] {
			p.prompted[key] = true
			masked := strings.Contains(strings.ToLower(m), "password")
			acts = append(acts, action{kind: actPrompt, line: strings.TrimSpace(p.tail), masked: masked})
			p.tail = ""
		}
	}
	return acts
}

func (p *steamParser) parseLine(line string) []action {
	p.lastLine = line
	if m := progressRe.FindStringSubmatch(line); m != nil {
		done, _ := strconv.ParseInt(m[2], 10, 64)
		total, _ := strconv.ParseInt(m[3], 10, 64)
		return []action{{kind: actProgress, verb: strings.TrimSpace(m[1]), done: done, total: total}}
	}
	if m := successRe.FindStringSubmatch(line); m != nil {
		p.installed[m[1]] = true
		return []action{{kind: actLog, line: line}}
	}
	if m := failureRe.FindStringSubmatch(line); m != nil {
		if p.failure == "" {
			for _, g := range m[1:] {
				if g != "" {
					p.failure = g
					break
				}
			}
		}
		return []action{{kind: actLog, line: line}}
	}
	// Suppress SteamCMD's noisy self-update spinner frames; surface the rest.
	if strings.HasPrefix(line, "[") && strings.Contains(line, "----") {
		return nil
	}
	return []action{{kind: actLog, line: line}}
}
