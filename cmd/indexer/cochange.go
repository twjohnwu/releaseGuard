package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"math"
	"os"
	"os/exec"
	"strconv"
	"time"

	"github.com/acme/releaseguard/internal/indexer/code/cochange"
	"github.com/acme/releaseguard/internal/storage"
)

func stepCochange(ctx context.Context, pool *storage.Pool, repoID int64, repoPath string) error {
	lookback := getLookbackDays()
	since := time.Now().Add(-time.Duration(lookback) * 24 * time.Hour).Format("2006-01-02")

	out, err := exec.CommandContext(ctx, "git", "-C", repoPath, "log",
		"--since="+since, "--name-only", "--pretty=format:>>>%H").Output()
	if err != nil {
		return err
	}

	var commits [][]string
	var current []string
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}
		if len(line) >= 3 && line[:3] == ">>>" {
			if len(current) > 0 {
				commits = append(commits, current)
			}
			current = nil
			continue
		}
		current = append(current, line)
	}
	if len(current) > 0 {
		commits = append(commits, current)
	}
	matrix := cochange.BuildMatrix(commits)

	for file, peers := range matrix {
		blameOut, err := exec.CommandContext(ctx, "git", "-C", repoPath,
			"blame", "--line-porcelain", file).Output()
		if err != nil {
			continue
		}
		authors, err := cochange.ParsePorcelain(bytes.NewReader(blameOut))
		if err != nil {
			continue
		}
		total := 0
		for _, n := range authors {
			total += n
		}
		if total == 0 {
			continue
		}
		recency := math.Exp(-1.0 / float64(lookback))
		ccJSON, _ := encodeCoChange(peers)
		for email, lines := range authors {
			weight := float64(lines) / float64(total)
			_, err := pool.Exec(ctx, `
				INSERT INTO ownership_signals(repo_id, file_path, author, blame_weight, co_change_files, recency_score)
				VALUES($1,$2,$3,$4,$5::jsonb,$6)
				ON CONFLICT (repo_id, file_path, author)
				DO UPDATE SET blame_weight=EXCLUDED.blame_weight,
				              co_change_files=EXCLUDED.co_change_files,
				              recency_score=EXCLUDED.recency_score,
				              computed_at=now()`,
				repoID, file, email, weight, ccJSON, recency)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func encodeCoChange(peers map[string]int) (string, error) {
	if len(peers) == 0 {
		return "[]", nil
	}
	type entry struct {
		File  string  `json:"file"`
		Score float64 `json:"score"`
	}
	total := 0
	for _, n := range peers {
		total += n
	}
	out := make([]entry, 0, len(peers))
	for f, n := range peers {
		out = append(out, entry{File: f, Score: float64(n) / float64(total)})
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func getLookbackDays() int {
	v := os.Getenv("OWNERSHIP_LOOKBACK_DAYS")
	if v == "" {
		return 180
	}
	n, _ := strconv.Atoi(v)
	if n <= 0 {
		return 180
	}
	return n
}
