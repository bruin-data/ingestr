package google_analytics

import (
	"context"
	"reflect"
	"testing"

	"github.com/bruin-data/ingestr/pkg/source"
)

func TestCustomReportPrimaryKeysUseAllDimensions(t *testing.T) {
	src := NewGoogleAnalyticsSource()

	table, err := src.GetTable(context.Background(), tableRequest("custom:date,sessionDefaultChannelGroup,sessionSource,sessionMedium,sessionCampaignName,country,region,city,landingPage:sessions,totalUsers,newUsers,engagedSessions,screenPageViews,userEngagementDuration"))
	if err != nil {
		t.Fatalf("GetTable returned error: %v", err)
	}

	expected := []string{
		"property_id",
		"date",
		"sessionDefaultChannelGroup",
		"sessionSource",
		"sessionMedium",
		"sessionCampaignName",
		"country",
		"region",
		"city",
		"landingPage",
	}
	if !reflect.DeepEqual(table.PrimaryKeys(), expected) {
		t.Fatalf("primary keys = %#v, want %#v", table.PrimaryKeys(), expected)
	}
}

func TestRealtimeReportPrimaryKeysUseDimensionsAndSnapshotFields(t *testing.T) {
	src := NewGoogleAnalyticsSource()

	table, err := src.GetTable(context.Background(), tableRequest("realtime:city,country:activeUsers"))
	if err != nil {
		t.Fatalf("GetTable returned error: %v", err)
	}

	expected := []string{"property_id", "city", "country", "ingested_at"}
	if !reflect.DeepEqual(table.PrimaryKeys(), expected) {
		t.Fatalf("primary keys = %#v, want %#v", table.PrimaryKeys(), expected)
	}
}

func TestRealtimeReportPrimaryKeysIncludeDateRangeForMultipleMinuteRanges(t *testing.T) {
	src := NewGoogleAnalyticsSource()

	table, err := src.GetTable(context.Background(), tableRequest("realtime:city:activeUsers:1-5,6-10"))
	if err != nil {
		t.Fatalf("GetTable returned error: %v", err)
	}

	expected := []string{"property_id", "city", "date_range", "ingested_at"}
	if !reflect.DeepEqual(table.PrimaryKeys(), expected) {
		t.Fatalf("primary keys = %#v, want %#v", table.PrimaryKeys(), expected)
	}
}

func TestParseConnectionURIAllowsMissingCredentials(t *testing.T) {
	credJSON, propertyIDs, err := parseConnectionURI("googleanalytics://?property_id=123")
	if err != nil {
		t.Fatalf("parseConnectionURI returned error: %v", err)
	}
	if credJSON != nil {
		t.Fatalf("credJSON = %v, want nil when no credentials provided", credJSON)
	}
	if !reflect.DeepEqual(propertyIDs, []string{"123"}) {
		t.Fatalf("propertyIDs = %#v", propertyIDs)
	}
}

func TestParseConnectionURIRequiresPropertyID(t *testing.T) {
	if _, _, err := parseConnectionURI("googleanalytics://"); err == nil {
		t.Fatal("expected error when property_id is missing, got nil")
	}
}

func tableRequest(name string) source.TableRequest {
	return source.TableRequest{Name: name}
}
