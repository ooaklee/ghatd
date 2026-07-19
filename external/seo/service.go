package seo

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// SitemapRepository defines the repository surface used by the service.
type SitemapRepository interface {
	CreateSitemapItem(ctx context.Context, item *SitemapItem) (*SitemapItem, error)
	DeleteSitemapItemsByURIs(ctx context.Context, uris []string) error
	GetAllSitemapItems(ctx context.Context) ([]SitemapItem, error)
	GetLatestSitemapItemByURIRegex(ctx context.Context, uriRegex string) (*SitemapItem, error)
	GetSitemapItemByURI(ctx context.Context, uri string) (*SitemapItem, error)
	GetSitemapItems(ctx context.Context, req *GetSitemapItemsRequest) ([]SitemapItem, error)
	GetTotalSitemapItems(ctx context.Context, req *GetSitemapItemsRequest) (int64, error)
	UpdateSitemapItemByURI(ctx context.Context, item *SitemapItem) (*SitemapItem, error)
	UpsertSitemapItemByURI(ctx context.Context, item *SitemapItem) (*SitemapItem, bool, error)
}

// Service holds and manages sitemap business logic.
type Service struct {
	SitemapRepository  SitemapRepository
	FrontendDomain     string
	FileSystem         fs.FS
	EmbeddedPathPrefix string
	WritableRootPath   string
	DefaultPath        string

	writeMutex sync.Mutex
}

// NewServiceRequest holds dependencies needed to initialise sitemap service.
type NewServiceRequest struct {
	SitemapRepository  SitemapRepository
	FrontendDomain     string
	FileSystem         fs.FS
	EmbeddedPathPrefix string
	WritableRootPath   string
	DefaultPath        string
}

// NewService creates a sitemap service.
func NewService(req *NewServiceRequest) *Service {
	if req == nil {
		req = &NewServiceRequest{}
	}

	defaultPath := strings.TrimSpace(req.DefaultPath)
	if defaultPath == "" {
		defaultPath = defaultSitemapPath
	}

	writableRootPath := strings.TrimSpace(req.WritableRootPath)
	if writableRootPath == "" {
		writableRootPath = defaultSitemapWritableRoot
	}

	return &Service{
		SitemapRepository:  req.SitemapRepository,
		FrontendDomain:     strings.TrimRight(strings.TrimSpace(req.FrontendDomain), "/"),
		FileSystem:         req.FileSystem,
		EmbeddedPathPrefix: strings.Trim(strings.TrimSpace(req.EmbeddedPathPrefix), "/"),
		WritableRootPath:   writableRootPath,
		DefaultPath:        defaultPath,
	}
}

// DefaultSitemapPath returns the configured default sitemap path.
func (s *Service) DefaultSitemapPath() string {
	if strings.TrimSpace(s.DefaultPath) == "" {
		return defaultSitemapPath
	}
	return s.DefaultPath
}

// CreateSitemapItemIfDoesNotAlreadyExist creates a sitemap item unless the URI already exists.
func (s *Service) CreateSitemapItemIfDoesNotAlreadyExist(ctx context.Context, req *CreateSitemapItemRequest) (*SitemapItemResponse, error) {
	item := NewSitemapItem(req)
	if item.CreatedByID == "" {
		item.CreatedByID = defaultIdentityTagSystem
	}
	if err := validateSitemapItem(item); err != nil {
		return nil, err
	}

	existing, err := s.SitemapRepository.GetSitemapItemByURI(ctx, item.URI)
	if err == nil {
		return &SitemapItemResponse{SitemapItem: existing, Created: false}, nil
	}
	if !errors.Is(err, ErrSitemapItemResourceNotFound) {
		return nil, err
	}

	item.GenerateID().SetCreatedAtTimeToNow()
	created, err := s.SitemapRepository.CreateSitemapItem(ctx, item)
	if err != nil {
		return nil, err
	}

	return &SitemapItemResponse{SitemapItem: created, Created: true}, nil
}

// MassSitemapItemCreationByBatch creates sitemap items in sequence with optional override.
func (s *Service) MassSitemapItemCreationByBatch(ctx context.Context, req *MassSitemapItemCreationByBatchRequest) (*MassSitemapItemCreationByBatchResponse, error) {
	if req == nil {
		return nil, ErrSitemapItemError
	}

	prepared := make([]*SitemapItem, 0, len(req.Items))
	for _, raw := range req.Items {
		item := NewSitemapItem(&raw)
		if item.CreatedByID == "" {
			item.CreatedByID = defaultIdentityTagSystem
		}
		if err := validateSitemapItem(item); err != nil {
			return nil, err
		}
		prepared = append(prepared, item)
	}

	response := &MassSitemapItemCreationByBatchResponse{}
	for _, item := range prepared {
		if req.OverrideIfExists {
			item.GenerateID().SetCreatedAtTimeToNow().SetUpdatedAtTimeToNow()
			upserted, created, err := s.SitemapRepository.UpsertSitemapItemByURI(ctx, item)
			if err != nil {
				return nil, err
			}
			response.SitemapItems = append(response.SitemapItems, *upserted)
			if created {
				response.Created++
			} else {
				response.Updated++
			}
			continue
		}

		created, err := s.CreateSitemapItemIfDoesNotAlreadyExist(ctx, &CreateSitemapItemRequest{
			URI:             item.URI,
			LastMod:         item.LastMod,
			Priority:        &item.Priority,
			ChangeFrequency: item.ChangeFrequency,
			CreatedByID:     item.CreatedByID,
		})
		if err != nil {
			return nil, err
		}
		response.SitemapItems = append(response.SitemapItems, *created.SitemapItem)
		if created.Created {
			response.Created++
		} else {
			response.Skipped++
		}
	}

	return response, nil
}

// GetLatestSitemapItemByURIRegex retrieves the newest sitemap item whose URI matches a safe regex.
func (s *Service) GetLatestSitemapItemByURIRegex(ctx context.Context, req *GetLatestSitemapItemByURIRegexRequest) (*SitemapItemResponse, error) {
	if req == nil || strings.TrimSpace(req.URIRegex) == "" {
		return nil, ErrSitemapItemURIIsRequired
	}

	uriRegex := strings.TrimSpace(req.URIRegex)
	if len(uriRegex) > maxSitemapURIRegexLength {
		return nil, ErrSitemapItemInvalidURI
	}
	if _, err := regexp.Compile(uriRegex); err != nil {
		return nil, ErrSitemapItemInvalidURI
	}

	item, err := s.SitemapRepository.GetLatestSitemapItemByURIRegex(ctx, uriRegex)
	if err != nil {
		return nil, err
	}

	return &SitemapItemResponse{SitemapItem: item}, nil
}

// GetSitemapItems retrieves sitemap items by filter.
func (s *Service) GetSitemapItems(ctx context.Context, req *GetSitemapItemsRequest) (*GetSitemapItemsResponse, error) {
	items, err := s.SitemapRepository.GetSitemapItems(ctx, req)
	if err != nil {
		return nil, err
	}

	total, err := s.SitemapRepository.GetTotalSitemapItems(ctx, req)
	if err != nil {
		return nil, err
	}

	return &GetSitemapItemsResponse{SitemapItems: items, Total: total}, nil
}

// UpdateSitemapItemByUri updates mutable fields on a sitemap item.
func (s *Service) UpdateSitemapItemByUri(ctx context.Context, req *UpdateSitemapItemRequest) (*SitemapItemResponse, error) {
	if req == nil || normaliseSitemapURI(req.URI) == "" {
		return nil, ErrSitemapItemURIIsRequired
	}

	current, err := s.SitemapRepository.GetSitemapItemByURI(ctx, req.URI)
	if err != nil {
		return nil, err
	}

	if req.LastMod != "" {
		current.LastMod = strings.TrimSpace(req.LastMod)
	}
	if req.Priority != nil {
		current.Priority = *req.Priority
	}
	if req.ChangeFrequency != nil {
		current.ChangeFrequency = NormaliseChangeFrequency(*req.ChangeFrequency)
	}
	current.UpdatedByID = strings.TrimSpace(req.UpdatedByID)
	current.SetUpdatedAtTimeToNow()

	if err = validateSitemapItem(current); err != nil {
		return nil, err
	}

	updated, err := s.SitemapRepository.UpdateSitemapItemByURI(ctx, current)
	if err != nil {
		return nil, err
	}

	return &SitemapItemResponse{SitemapItem: updated, Updated: true}, nil
}

// DeleteEntriesWithUriRegex deletes sitemap entries whose URI matches a safe Go regular expression.
func (s *Service) DeleteEntriesWithUriRegex(ctx context.Context, req *DeleteEntriesWithURIRegexRequest) (*DeleteEntriesWithURIRegexResponse, error) {
	if req == nil || strings.TrimSpace(req.URIRegex) == "" {
		return nil, ErrSitemapItemURIIsRequired
	}

	uriRegex := strings.TrimSpace(req.URIRegex)
	if len(uriRegex) > maxSitemapURIRegexLength {
		return nil, ErrSitemapItemInvalidURI
	}

	matcher, err := regexp.Compile(uriRegex)
	if err != nil {
		return nil, ErrSitemapItemInvalidURI
	}

	items, err := s.SitemapRepository.GetAllSitemapItems(ctx)
	if err != nil {
		return nil, err
	}

	uris := make([]string, 0)
	for _, item := range items {
		if matcher.MatchString(item.URI) {
			uris = append(uris, item.URI)
		}
	}

	if err = s.SitemapRepository.DeleteSitemapItemsByURIs(ctx, uris); err != nil {
		return nil, err
	}

	return &DeleteEntriesWithURIRegexResponse{Deleted: int64(len(uris)), URIs: uris}, nil
}

// GenerateSitemap generates sitemap XML from stored items and optionally saves it to paths.
func (s *Service) GenerateSitemap(ctx context.Context, req *GenerateSitemapRequest) (*GenerateSitemapResponse, error) {
	if strings.TrimSpace(s.FrontendDomain) == "" {
		return nil, ErrSitemapFrontendDomainIsRequired
	}

	items, err := s.SitemapRepository.GetAllSitemapItems(ctx)
	if err != nil {
		return nil, err
	}

	generatedXML, err := s.GenerateXML(items)
	if err != nil {
		return nil, err
	}

	response := &GenerateSitemapResponse{XML: generatedXML, Total: strings.Count(generatedXML, "<url>")}
	if req != nil {
		for _, savePath := range req.SaveToPaths {
			if strings.TrimSpace(savePath) == "" {
				continue
			}
			if err = s.writeSitemapFile(savePath, []byte(generatedXML)); err != nil {
				return nil, err
			}
			response.SavedPaths = append(response.SavedPaths, savePath)
		}
	}

	return response, nil
}

// GenerateXML renders sitemap XML for the provided items.
func (s *Service) GenerateXML(items []SitemapItem) (string, error) {
	urls := make([]sitemapURLEntry, 0, len(items))
	seenLocs := map[string]struct{}{}
	for _, item := range items {
		if shouldExcludeSitemapURI(item.URI) {
			continue
		}
		if err := validateSitemapItem(&item); err != nil {
			return "", err
		}

		lastMod, err := item.LastModForXML()
		if err != nil {
			return "", err
		}

		loc, err := s.buildLoc(item.URI)
		if err != nil {
			return "", err
		}
		if _, alreadySeen := seenLocs[loc]; alreadySeen {
			continue
		}
		seenLocs[loc] = struct{}{}

		urls = append(urls, sitemapURLEntry{
			Loc:        loc,
			LastMod:    lastMod,
			ChangeFreq: item.ChangeFrequency.String(),
			Priority:   item.FormatPriority(),
		})
	}
	sort.Slice(urls, func(i, j int) bool {
		return urls[i].Loc < urls[j].Loc
	})

	doc := sitemapURLSet{
		Xmlns: "http://www.sitemaps.org/schemas/sitemap/0.9",
		URLs:  urls,
	}

	var buffer bytes.Buffer
	buffer.WriteString(xml.Header)
	encoder := xml.NewEncoder(&buffer)
	if err := encoder.Encode(doc); err != nil {
		return "", err
	}
	if err := encoder.Flush(); err != nil {
		return "", err
	}

	return buffer.String(), nil
}

func shouldExcludeSitemapURI(uri string) bool {
	uri = normaliseSitemapURI(uri)
	parsed, err := url.Parse(uri)
	if err != nil {
		return false
	}

	path := parsed.Path
	if path == "" {
		path = uri
	}
	path = strings.TrimRight(path, "/")
	if path == "" {
		path = "/"
	}

	excludedPrefixes := []string{
		"/admin",
		"/api",
		"/app",
		"/auth",
		"/offline",
		"/portal",
		"/settings",
	}
	for _, prefix := range excludedPrefixes {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}

	return false
}

// DownloadSitemapByPath returns sitemap file content from a safe local path or embedded fallback.
func (s *Service) DownloadSitemapByPath(_ context.Context, req *DownloadSitemapByPathRequest) (*DownloadSitemapByPathResponse, error) {
	requestedPath := s.DefaultSitemapPath()
	if req != nil && strings.TrimSpace(req.Path) != "" {
		requestedPath = req.Path
	}

	content, cleanPath, err := s.readSitemapFile(requestedPath)
	if err != nil {
		return nil, err
	}

	return &DownloadSitemapByPathResponse{
		Path:        cleanPath,
		FileName:    path.Base(cleanPath),
		ContentType: "application/xml; charset=utf-8",
		Content:     content,
	}, nil
}

func (s *Service) buildLoc(uri string) (string, error) {
	uri = normaliseSitemapURI(uri)
	parsed, err := url.Parse(uri)
	if err != nil {
		return "", ErrSitemapItemInvalidURI
	}
	if parsed.IsAbs() {
		return parsed.String(), nil
	}

	base, err := url.Parse(strings.TrimRight(s.FrontendDomain, "/") + "/")
	if err != nil || base.Scheme == "" || base.Host == "" {
		return "", ErrSitemapFrontendDomainIsRequired
	}

	return base.ResolveReference(parsed).String(), nil
}

func (s *Service) readSitemapFile(requestedPath string) ([]byte, string, error) {
	cleanPath, err := cleanSitemapPath(requestedPath)
	if err != nil {
		return nil, "", err
	}

	fullPath, err := s.resolveWritablePath(cleanPath)
	if err != nil {
		return nil, "", err
	}

	content, err := os.ReadFile(fullPath)
	if err == nil {
		return content, cleanPath, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, "", err
	}
	if s.FileSystem == nil {
		return nil, "", err
	}

	embeddedPath := cleanPath
	if s.EmbeddedPathPrefix != "" {
		embeddedPath = path.Join(s.EmbeddedPathPrefix, cleanPath)
	}

	content, err = fs.ReadFile(s.FileSystem, embeddedPath)
	if err != nil {
		return nil, "", err
	}

	return content, cleanPath, nil
}

func (s *Service) writeSitemapFile(requestedPath string, content []byte) error {
	cleanPath, err := cleanSitemapPath(requestedPath)
	if err != nil {
		return err
	}

	fullPath, err := s.resolveWritablePath(cleanPath)
	if err != nil {
		return err
	}

	s.writeMutex.Lock()
	defer s.writeMutex.Unlock()

	if err = os.MkdirAll(filepath.Dir(fullPath), defaultSitemapDirectoryPerm); err != nil {
		return err
	}

	tmpFile, err := os.CreateTemp(filepath.Dir(fullPath), ".sitemap-*.xml")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	defer func() {
		_ = os.Remove(tmpPath)
	}()

	if _, err = tmpFile.Write(content); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if err = tmpFile.Chmod(defaultSitemapFilePerm); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if err = tmpFile.Close(); err != nil {
		return err
	}

	return os.Rename(tmpPath, fullPath)
}

func (s *Service) resolveWritablePath(cleanPath string) (string, error) {
	root := strings.TrimSpace(s.WritableRootPath)
	if root == "" {
		root = defaultSitemapWritableRoot
	}

	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}

	fullPath := filepath.Join(absoluteRoot, filepath.FromSlash(cleanPath))
	absoluteFullPath, err := filepath.Abs(fullPath)
	if err != nil {
		return "", err
	}

	if absoluteFullPath != absoluteRoot && !strings.HasPrefix(absoluteFullPath, absoluteRoot+string(os.PathSeparator)) {
		return "", ErrSitemapPathIsInvalid
	}

	return absoluteFullPath, nil
}

func cleanSitemapPath(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = defaultSitemapPath
	}
	if filepath.IsAbs(value) {
		return "", ErrSitemapPathIsInvalid
	}

	cleaned := path.Clean(strings.TrimLeft(filepath.ToSlash(value), "/"))
	if cleaned == "." || cleaned == "" || strings.HasPrefix(cleaned, "../") || cleaned == ".." {
		return "", ErrSitemapPathIsInvalid
	}

	return cleaned, nil
}

type sitemapURLSet struct {
	XMLName xml.Name          `xml:"urlset"`
	Xmlns   string            `xml:"xmlns,attr"`
	URLs    []sitemapURLEntry `xml:"url"`
}

type sitemapURLEntry struct {
	Loc        string `xml:"loc"`
	LastMod    string `xml:"lastmod"`
	ChangeFreq string `xml:"changefreq"`
	Priority   string `xml:"priority"`
}
