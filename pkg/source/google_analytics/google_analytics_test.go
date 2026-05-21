package google_analytics

import (
	"context"
	"reflect"
	"testing"

	"github.com/bruin-data/gong/pkg/source"
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

func tableRequest(name string) source.TableRequest {
	return source.TableRequest{Name: name}
}
