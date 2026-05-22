package chess

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bruin-data/ingestr/internal/config"
	"github.com/bruin-data/ingestr/pkg/arrowconv"
	httpclient "github.com/bruin-data/ingestr/pkg/http"
	"github.com/bruin-data/ingestr/pkg/schema"
	"github.com/bruin-data/ingestr/pkg/source"
)

const (
	baseURL        = "https://api.chess.com/pub"
	defaultPlayers = "hikaru,magnuscarlsen,gothamchess,fabianocaruana"
)

var profileColumns = []schema.Column{
	{Name: "joined", DataType: schema.TypeTimestampTZ, Nullable: true},
	{Name: "last_online", DataType: schema.TypeTimestampTZ, Nullable: true},
}

var gameColumns = []schema.Column{
	{Name: "end_time", DataType: schema.TypeTimestampTZ, Nullable: true},
}

type ChessSource struct {
	players []string
	client  *httpclient.Client
	tables  map[string]source.SourceTable
}

func NewChessSource() *ChessSource {
	return &ChessSource{
		client: httpclient.New(
			httpclient.WithBaseURL(baseURL),
			httpclient.WithTimeout(30*time.Second),
			httpclient.WithRateLimiter(5, 2),
			httpclient.WithDebug(config.DebugMode),
		),
	}
}

func (s *ChessSource) Schemes() []string {
	return []string{"chess"}
}

func (s *ChessSource) Connect(ctx context.Context, uri string) error {
	players, err := parsePlayersFromURI(uri)
	if err != nil {
		return err
	}
	s.players = players
	s.tables = s.getTables()
	config.Debug("[CHESS] Connected with players: %v", s.players)
	return nil
}

func parsePlayersFromURI(uri string) ([]string, error) {
	if !strings.HasPrefix(uri, "chess://") {
		return nil, fmt.Errorf("invalid chess URI: must start with chess://")
	}

	rest := strings.TrimPrefix(uri, "chess://")
	if rest == "" || rest == "?" {
		return strings.Split(defaultPlayers, ","), nil
	}

	rest = strings.TrimPrefix(rest, "?")

	values, err := url.ParseQuery(rest)
	if err != nil {
		return nil, fmt.Errorf("failed to parse chess URI query: %w", err)
	}

	playersParam := values.Get("players")
	if playersParam == "" {
		return strings.Split(defaultPlayers, ","), nil
	}

	players := strings.Split(playersParam, ",")
	for i := range players {
		players[i] = strings.TrimSpace(players[i])
	}

	var filtered []string
	for _, p := range players {
		if p != "" {
			filtered = append(filtered, p)
		}
	}

	if len(filtered) == 0 {
		return strings.Split(defaultPlayers, ","), nil
	}

	return filtered, nil
}

func (s *ChessSource) Close(ctx context.Context) error {
	return s.client.Close()
}

func (s *ChessSource) HandlesIncrementality() bool {
	return true
}

func (s *ChessSource) getTables() map[string]source.SourceTable {
	return map[string]source.SourceTable{
		"profiles": &source.DynamicSourceTable{
			TableName:           "profiles",
			TablePrimaryKeys:    []string{},
			TableIncrementalKey: "",
			TableStrategy:       config.StrategyReplace,
			KnownSchema:         false,
			SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
				return nil, fmt.Errorf("chess source does not have a predefined schema; schema inference is required")
			},
			ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
				return s.readTable(ctx, s.readProfiles, opts)
			},
		},
		"games": &source.DynamicSourceTable{
			TableName:           "games",
			TablePrimaryKeys:    []string{},
			TableIncrementalKey: "",
			TableStrategy:       config.StrategyReplace,
			KnownSchema:         false,
			SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
				return nil, fmt.Errorf("chess source does not have a predefined schema; schema inference is required")
			},
			ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
				return s.readTable(ctx, s.readGames, opts)
			},
		},
		"archives": &source.DynamicSourceTable{
			TableName:           "archives",
			TablePrimaryKeys:    []string{},
			TableIncrementalKey: "",
			TableStrategy:       config.StrategyReplace,
			KnownSchema:         false,
			SchemaFn: func(ctx context.Context) (*schema.TableSchema, error) {
				return nil, fmt.Errorf("chess source does not have a predefined schema; schema inference is required")
			},
			ReadFn: func(ctx context.Context, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
				return s.readTable(ctx, s.readArchives, opts)
			},
		},
	}
}

func (s *ChessSource) GetTable(ctx context.Context, req source.TableRequest) (source.SourceTable, error) {
	table, ok := s.tables[req.Name]
	if !ok {
		return nil, fmt.Errorf("unsupported table: %s (supported: profiles, games, archives)", req.Name)
	}
	return table, nil
}

// tableReader is a function that reads data for a specific table.
type tableReader func(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error

func (s *ChessSource) readTable(ctx context.Context, reader tableReader, opts source.ReadOptions) (<-chan source.RecordBatchResult, error) {
	results := make(chan source.RecordBatchResult, 8)

	go func() {
		defer close(results)

		if err := reader(ctx, opts, results); err != nil {
			results <- source.RecordBatchResult{Err: err}
		}
	}()

	return results, nil
}

func (s *ChessSource) fetch(ctx context.Context, endpoint string) (map[string]interface{}, error) {
	config.Debug("[CHESS] Fetching: %s%s", baseURL, endpoint)

	var result map[string]interface{}
	resp, err := s.client.R(ctx).
		SetResult(&result).
		Get(endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch %s: %w", endpoint, err)
	}

	if resp.StatusCode() == http.StatusNotFound {
		return nil, nil
	}

	if !resp.IsSuccess() {
		return nil, fmt.Errorf("API returned status %d for %s", resp.StatusCode(), endpoint)
	}

	return result, nil
}

func (s *ChessSource) readProfiles(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[CHESS] Reading profiles for %d players", len(s.players))

	var profiles []map[string]interface{}
	for _, player := range s.players {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		profile, err := s.fetch(ctx, fmt.Sprintf("/player/%s", player))
		if err != nil {
			config.Debug("[CHESS] Error fetching profile for %s: %v", player, err)
			continue
		}
		if profile == nil {
			config.Debug("[CHESS] Player not found: %s", player)
			continue
		}
		profiles = append(profiles, profile)
	}

	if len(profiles) == 0 {
		config.Debug("[CHESS] No profiles found")
		return nil
	}

	record, err := arrowconv.ItemsToArrowRecordWithSchema(profiles, profileColumns, opts.ExcludeColumns)
	if err != nil {
		return fmt.Errorf("failed to convert profiles to Arrow: %w", err)
	}

	results <- source.RecordBatchResult{Batch: record}
	config.Debug("[CHESS] Sent %d profiles", len(profiles))
	return nil
}

type archiveFetchTask struct {
	player     string
	archiveURL string
}

type archiveFetchResult struct {
	player string
	games  []map[string]interface{}
	err    error
}

var archiveURLPattern = regexp.MustCompile(`/games/(\d{4})/(\d{2})$`)

func parseArchiveDate(archiveURL string) (year, month int, ok bool) {
	matches := archiveURLPattern.FindStringSubmatch(archiveURL)
	if len(matches) != 3 {
		return 0, 0, false
	}

	year, err := strconv.Atoi(matches[1])
	if err != nil {
		return 0, 0, false
	}

	month, err = strconv.Atoi(matches[2])
	if err != nil {
		return 0, 0, false
	}

	return year, month, true
}

func isArchiveInInterval(archiveURL string, intervalStart, intervalEnd interface{}) bool {
	year, month, ok := parseArchiveDate(archiveURL)
	if !ok {
		return true
	}

	archiveStart := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	archiveEnd := archiveStart.AddDate(0, 1, 0).Add(-time.Nanosecond)

	if intervalStart != nil {
		var startTime time.Time
		var hasStart bool
		switch v := intervalStart.(type) {
		case time.Time:
			startTime = v
			hasStart = !v.IsZero()
		case *time.Time:
			if v != nil {
				startTime = *v
				hasStart = !v.IsZero()
			}
		}
		if hasStart && archiveEnd.Before(startTime) {
			return false
		}
	}

	if intervalEnd != nil {
		var endTime time.Time
		var hasEnd bool
		switch v := intervalEnd.(type) {
		case time.Time:
			endTime = v
			hasEnd = !v.IsZero()
		case *time.Time:
			if v != nil {
				endTime = *v
				hasEnd = !v.IsZero()
			}
		}
		if hasEnd && archiveStart.After(endTime) {
			return false
		}
	}

	return true
}

func (s *ChessSource) readGames(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[CHESS] Reading games for %d players", len(s.players))

	batchSize := opts.PageSize
	if batchSize <= 0 {
		batchSize = 1000
	}

	parallelism := opts.Parallelism
	if parallelism <= 0 {
		parallelism = 5
	}

	var allTasks []archiveFetchTask
	for _, player := range s.players {
		archives, err := s.fetch(ctx, fmt.Sprintf("/player/%s/games/archives", player))
		if err != nil {
			config.Debug("[CHESS] Error fetching archives for %s: %v", player, err)
			continue
		}
		if archives == nil {
			config.Debug("[CHESS] No archives found for: %s", player)
			continue
		}

		archiveURLs, ok := archives["archives"].([]interface{})
		if !ok {
			continue
		}

		for i := len(archiveURLs) - 1; i >= 0; i-- {
			archiveURL, ok := archiveURLs[i].(string)
			if !ok {
				continue
			}

			if !isArchiveInInterval(archiveURL, opts.IntervalStart, opts.IntervalEnd) {
				config.Debug("[CHESS] Skipping archive outside interval: %s", archiveURL)
				continue
			}

			allTasks = append(allTasks, archiveFetchTask{player: player, archiveURL: archiveURL})
		}
	}

	if len(allTasks) == 0 {
		config.Debug("[CHESS] No archives to fetch (after interval filtering)")
		return nil
	}

	config.Debug("[CHESS] Fetching %d archives with parallelism %d", len(allTasks), parallelism)

	taskChan := make(chan archiveFetchTask, len(allTasks))
	resultChan := make(chan archiveFetchResult, parallelism*2)

	var wg sync.WaitGroup
	for i := 0; i < parallelism; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for task := range taskChan {
				select {
				case <-ctx.Done():
					return
				default:
				}

				endpoint := strings.TrimPrefix(task.archiveURL, baseURL)
				monthData, err := s.fetch(ctx, endpoint)
				if err != nil {
					config.Debug("[CHESS] Error fetching games from %s: %v", endpoint, err)
					resultChan <- archiveFetchResult{player: task.player, err: err}
					continue
				}
				if monthData == nil {
					continue
				}

				gamesRaw, ok := monthData["games"].([]interface{})
				if !ok {
					continue
				}

				var games []map[string]interface{}
				for _, g := range gamesRaw {
					game, ok := g.(map[string]interface{})
					if !ok {
						continue
					}
					game["player"] = task.player
					games = append(games, game)
				}

				if len(games) > 0 {
					resultChan <- archiveFetchResult{player: task.player, games: games}
				}
			}
		}()
	}

	go func() {
		for _, task := range allTasks {
			taskChan <- task
		}
		close(taskChan)
	}()

	go func() {
		wg.Wait()
		close(resultChan)
	}()

	totalLimit := opts.Limit
	totalSent := 0
	batchNum := 0
	var pendingGames []map[string]interface{}

	sendBatch := func() error {
		if len(pendingGames) == 0 {
			return nil
		}

		record, err := arrowconv.ItemsToArrowRecordWithSchema(pendingGames, gameColumns, opts.ExcludeColumns)
		if err != nil {
			return fmt.Errorf("failed to convert games to Arrow: %w", err)
		}

		batchNum++
		config.Debug("[CHESS] Sending batch %d with %d games (total sent: %d)", batchNum, len(pendingGames), totalSent+len(pendingGames))
		results <- source.RecordBatchResult{Batch: record}
		totalSent += len(pendingGames)
		pendingGames = nil
		return nil
	}

	for result := range resultChan {
		if result.err != nil {
			continue
		}

		for _, game := range result.games {
			if totalLimit > 0 && totalSent+len(pendingGames) >= totalLimit {
				break
			}

			pendingGames = append(pendingGames, game)

			if len(pendingGames) >= batchSize {
				if err := sendBatch(); err != nil {
					return err
				}
			}
		}

		if totalLimit > 0 && totalSent >= totalLimit {
			break
		}
	}

	if err := sendBatch(); err != nil {
		return err
	}

	if totalSent == 0 {
		config.Debug("[CHESS] No games found")
	}

	return nil
}

func (s *ChessSource) readArchives(ctx context.Context, opts source.ReadOptions, results chan<- source.RecordBatchResult) error {
	config.Debug("[CHESS] Reading archives for %d players", len(s.players))

	var archives []map[string]interface{}
	for _, player := range s.players {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		data, err := s.fetch(ctx, fmt.Sprintf("/player/%s/games/archives", player))
		if err != nil {
			config.Debug("[CHESS] Error fetching archives for %s: %v", player, err)
			continue
		}
		if data == nil {
			config.Debug("[CHESS] No archives found for: %s", player)
			continue
		}

		archiveURLs, ok := data["archives"].([]interface{})
		if !ok {
			continue
		}

		for _, urlVal := range archiveURLs {
			url, ok := urlVal.(string)
			if !ok {
				continue
			}
			archives = append(archives, map[string]interface{}{
				"player":      player,
				"archive_url": url,
			})
		}
	}

	if len(archives) == 0 {
		config.Debug("[CHESS] No archives found")
		return nil
	}

	record, err := arrowconv.ItemsToArrowRecordWithSchema(archives, nil, opts.ExcludeColumns)
	if err != nil {
		return fmt.Errorf("failed to convert archives to Arrow: %w", err)
	}

	results <- source.RecordBatchResult{Batch: record}
	config.Debug("[CHESS] Sent %d archive entries", len(archives))
	return nil
}

var _ source.Source = (*ChessSource)(nil)
