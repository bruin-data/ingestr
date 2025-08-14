from typing import Iterable, Optional

import dlt
import pendulum
from dlt.sources import DltResource

from .helpers import create_dynamic_resource, get_access_token

# Define available resources with their endpoints and field mappings
FLUXX_RESOURCES = {
    "claim": {
        "endpoint": "claim",
        "fields": {
            # Primary key
            "id": {"data_type": "bigint", "field_type": "column"},
            # Amount fields (decimal for monetary values)
            "amount_recommended": {"data_type": "decimal", "field_type": "column"},
            "amount_requested": {"data_type": "decimal", "field_type": "column"},
            # Boolean fields
            "approved": {"data_type": "bool", "field_type": "column"},
            "is_temp_precreate": {"data_type": "bool", "field_type": "column"},
            "submitted": {"data_type": "bool", "field_type": "column"},
            # Timestamp fields
            "approved_at": {"data_type": "timestamp", "field_type": "column"},
            "created_at": {"data_type": "timestamp", "field_type": "column"},
            "deleted_at": {"data_type": "timestamp", "field_type": "column"},
            "due_at": {"data_type": "timestamp", "field_type": "column"},
            "locked_until": {"data_type": "timestamp", "field_type": "column"},
            "submitted_at": {"data_type": "timestamp", "field_type": "column"},
            "updated_at": {"data_type": "timestamp", "field_type": "timestamp"},
            # Date fields
            "timestamp_entered_state": {"data_type": "date", "field_type": "column"},
            # Integer/ID fields
            "contig_id": {"data_type": "bigint", "field_type": "column"},
            "locked_by_id": {"data_type": "bigint", "field_type": "column"},
            "migrate_id": {"data_type": "bigint", "field_type": "column"},
            # Text fields
            "migrate_source_name": {"data_type": "text", "field_type": "column"},
            "state": {"data_type": "text", "field_type": "column"},
            # String/Text fields
            "all_notes": {"data_type": "text", "field_type": "string"},
            "filter_state": {"data_type": "text", "field_type": "string"},
            "grant_request_workflow_state": {
                "data_type": "text",
                "field_type": "string",
            },
            "previous_state": {"data_type": "text", "field_type": "string"},
            "request_grant_id": {"data_type": "text", "field_type": "string"},
            "request_grantee_user_name": {"data_type": "text", "field_type": "string"},
            "request_org_acronym": {"data_type": "text", "field_type": "string"},
            "request_org_name": {"data_type": "text", "field_type": "string"},
            "state_description": {"data_type": "text", "field_type": "string"},
            "state_to_english": {"data_type": "text", "field_type": "string"},
            "to_s": {"data_type": "text", "field_type": "string"},
            # Relation fields (stored as bigint for IDs or json for arrays)
            "alert_emails": {"data_type": "json", "field_type": "relation"},
            "alert_ids_sent": {"data_type": "json", "field_type": "relation"},
            "audit_soft_deletes": {"data_type": "json", "field_type": "relation"},
            "claim_expenses": {"data_type": "json", "field_type": "relation"},
            "clone_ancestries": {"data_type": "json", "field_type": "relation"},
            "code_block_conversions": {"data_type": "json", "field_type": "relation"},
            "connection_ids": {"data_type": "json", "field_type": "relation"},
            "created_by": {"data_type": "bigint", "field_type": "relation"},
            "created_by_id": {"data_type": "bigint", "field_type": "relation"},
            "etl_claim_expense_data": {"data_type": "json", "field_type": "relation"},
            "etl_relationships": {"data_type": "json", "field_type": "relation"},
            "favorite_user_ids": {"data_type": "json", "field_type": "relation"},
            "favorites": {"data_type": "json", "field_type": "relation"},
            "fiscal_org_geo_country_id": {
                "data_type": "bigint",
                "field_type": "relation",
            },
            "fiscal_org_geo_state_id": {
                "data_type": "bigint",
                "field_type": "relation",
            },
            "geo_place_relationships": {"data_type": "json", "field_type": "relation"},
            "grant": {"data_type": "bigint", "field_type": "relation"},
            "grant_request": {"data_type": "bigint", "field_type": "relation"},
            "grant_request_model_theme_id": {
                "data_type": "bigint",
                "field_type": "relation",
            },
            "group_members": {"data_type": "json", "field_type": "relation"},
            "model_documents": {"data_type": "json", "field_type": "relation"},
            "model_emails": {"data_type": "json", "field_type": "relation"},
            "model_summaries": {"data_type": "json", "field_type": "relation"},
            "model_theme": {"data_type": "bigint", "field_type": "relation"},
            "model_theme_id": {"data_type": "bigint", "field_type": "relation"},
            "modifications": {"data_type": "json", "field_type": "relation"},
            "notes": {"data_type": "json", "field_type": "relation"},
            "post_relationships": {"data_type": "json", "field_type": "relation"},
            "program_org_geo_country_id": {
                "data_type": "bigint",
                "field_type": "relation",
            },
            "program_org_geo_state_id": {
                "data_type": "bigint",
                "field_type": "relation",
            },
            "rd_tab_grant_requests_grant": {
                "data_type": "json",
                "field_type": "relation",
            },
            "rd_tab_grant_requests_pri_grant": {
                "data_type": "json",
                "field_type": "relation",
            },
            "rd_tab_grant_requests_pri_request": {
                "data_type": "json",
                "field_type": "relation",
            },
            "rd_tab_grant_requests_request": {
                "data_type": "json",
                "field_type": "relation",
            },
            "rd_tab_grant_requests_request_or_grant": {
                "data_type": "json",
                "field_type": "relation",
            },
            "relationships": {"data_type": "json", "field_type": "relation"},
            "request_id": {"data_type": "bigint", "field_type": "relation"},
            "taggings": {"data_type": "json", "field_type": "relation"},
            "translator_assignments": {"data_type": "json", "field_type": "relation"},
            "updated_by": {"data_type": "bigint", "field_type": "relation"},
            "updated_by_id": {"data_type": "bigint", "field_type": "relation"},
            "work_tasks": {"data_type": "json", "field_type": "relation"},
            "workflow_events": {"data_type": "json", "field_type": "relation"},
        },
    },
    "organization": {
        "endpoint": "organization",
        "fields": {
            # Primary key
            "id": {"data_type": "bigint", "field_type": "column"},
            
            # Core text fields
            "name": {"data_type": "text", "field_type": "column"},
            "acronym": {"data_type": "text", "field_type": "column"},
            "legal_name": {"data_type": "text", "field_type": "column"},
            "name_foreign_language": {"data_type": "text", "field_type": "column"},
            
            # Boolean fields
            "active": {"data_type": "bool", "field_type": "column"},
            "avoid_list": {"data_type": "bool", "field_type": "column"},
            "c3_status_approved": {"data_type": "bool", "field_type": "column"},
            "c3_status_checked": {"data_type": "bool", "field_type": "column"},
            "can_receive_international_funding": {"data_type": "bool", "field_type": "column"},
            "chapter_sig_eligible_for_funding": {"data_type": "text", "field_type": "column"},
            "deny_list_overridden": {"data_type": "bool", "field_type": "column"},
            "ed_needed": {"data_type": "text", "field_type": "column"},
            "equivalency_determination_on_file": {"data_type": "bool", "field_type": "column"},
            "is_grantee": {"data_type": "bool", "field_type": "boolean"},
            "is_grantor": {"data_type": "bool", "field_type": "boolean"},
            "is_fs_or_so": {"data_type": "text", "field_type": "column"},
            "is_org_501c3": {"data_type": "bool", "field_type": "column"},
            "is_org_chapter": {"data_type": "bool", "field_type": "column"},
            "is_org_chapter_non_profit": {"data_type": "bool", "field_type": "column"},
            "is_sponsored_org": {"data_type": "bool", "field_type": "column"},
            "is_temp_precreate": {"data_type": "bool", "field_type": "column"},
            "cc_pub78_verified": {"data_type": "bool", "field_type": "column"},
            "system_generated_denylist": {"data_type": "bool", "field_type": "column"},
            
            # Timestamp fields
            "created_at": {"data_type": "timestamp", "field_type": "column"},
            "updated_at": {"data_type": "timestamp", "field_type": "column"},
            "deleted_at": {"data_type": "timestamp", "field_type": "column"},
            "c3_checked_at": {"data_type": "timestamp", "field_type": "column"},
            "cc_checked_at": {"data_type": "timestamp", "field_type": "column"},
            "demographic_data_last_modified_at": {"data_type": "timestamp", "field_type": "column"},
            "ext_sync_at": {"data_type": "timestamp", "field_type": "column"},
            "locked_until": {"data_type": "timestamp", "field_type": "column"},
            "ofac_run_at": {"data_type": "timestamp", "field_type": "column"},
            
            # Date fields
            "chapter_election_date": {"data_type": "date", "field_type": "column"},
            "ed_expiration_date": {"data_type": "date", "field_type": "column"},
            "ed_submission_date": {"data_type": "date", "field_type": "column"},
            "fx_date_1": {"data_type": "date", "field_type": "column"},
            "fx_date_2": {"data_type": "date", "field_type": "column"},
            "fx_applied_date_1": {"data_type": "date", "field_type": "column"},
            "fx_applied_date_2": {"data_type": "date", "field_type": "column"},
            "cc_reinstatement_date": {"data_type": "date", "field_type": "column"},
            "cc_revocation_date": {"data_type": "date", "field_type": "column"},
            "tax_period": {"data_type": "date", "field_type": "column"},
            "tax_registration_date": {"data_type": "date", "field_type": "column"},
            "timestamp_entered_state": {"data_type": "date", "field_type": "date"},
            
            # Address fields
            "street_address": {"data_type": "text", "field_type": "column"},
            "street_address2": {"data_type": "text", "field_type": "column"},
            "city": {"data_type": "text", "field_type": "column"},
            "state": {"data_type": "text", "field_type": "column"},
            "postal_code": {"data_type": "text", "field_type": "column"},
            
            # Contact fields
            "email": {"data_type": "text", "field_type": "column"},
            "phone": {"data_type": "text", "field_type": "column"},
            "phone_extension": {"data_type": "text", "field_type": "column"},
            "other_contact": {"data_type": "text", "field_type": "column"},
            "fax": {"data_type": "text", "field_type": "column"},
            
            # URL fields
            "url": {"data_type": "text", "field_type": "column"},
            "blog_url": {"data_type": "text", "field_type": "column"},
            "twitter_url": {"data_type": "text", "field_type": "column"},
            
            # Geographic coordinates
            "latitude": {"data_type": "double", "field_type": "column"},
            "longitude": {"data_type": "double", "field_type": "column"},
            
            # Numeric fields
            "contig_id": {"data_type": "bigint", "field_type": "column"},
            "fx_spotrate_1": {"data_type": "decimal", "field_type": "column"},
            "fx_spotrate_2": {"data_type": "decimal", "field_type": "column"},
            "fx_type_id": {"data_type": "bigint", "field_type": "column"},
            "locked_by_id": {"data_type": "bigint", "field_type": "column"},
            "migrate_id": {"data_type": "bigint", "field_type": "column"},
            "ofac_result_id": {"data_type": "bigint", "field_type": "column"},
            "ofac_run_id": {"data_type": "bigint", "field_type": "column"},
            "ofac_score": {"data_type": "decimal", "field_type": "column"},
            "primary_user_organization_id": {"data_type": "bigint", "field_type": "column"},
            "program_id": {"data_type": "bigint", "field_type": "column"},
            "segment_id": {"data_type": "bigint", "field_type": "column"},
            "translation_priority": {"data_type": "bigint", "field_type": "long"},
            "year_founded": {"data_type": "bigint", "field_type": "column"},
            
            # Tax and compliance fields
            "tax_id": {"data_type": "text", "field_type": "column"},
            "tax_class": {"data_type": "text", "field_type": "column"},
            "tax_notes": {"data_type": "text", "field_type": "column"},
            "duns_number": {"data_type": "text", "field_type": "column"},
            "vendor_number": {"data_type": "text", "field_type": "column"},
            "deductibility": {"data_type": "text", "field_type": "column"},
            "foundation": {"data_type": "text", "field_type": "column"},
            "subsection": {"data_type": "text", "field_type": "column"},
            
            # Charity Check fields
            "cc_affiliation_code": {"data_type": "text", "field_type": "column"},
            "cc_affiliation_code_description": {"data_type": "text", "field_type": "column"},
            "cc_bmf_status": {"data_type": "text", "field_type": "column"},
            "cc_bmf_subsection": {"data_type": "text", "field_type": "column"},
            "cc_charity_check_last_modified": {"data_type": "text", "field_type": "column"},
            "cc_checked_by": {"data_type": "text", "field_type": "column"},
            "cc_deductibility_code": {"data_type": "text", "field_type": "column"},
            "cc_deductibility_code_description": {"data_type": "text", "field_type": "column"},
            "cc_foundation_509a_status": {"data_type": "text", "field_type": "column"},
            "cc_foundation_code": {"data_type": "text", "field_type": "column"},
            "cc_foundation_code_description": {"data_type": "text", "field_type": "column"},
            "cc_foundation_type_code": {"data_type": "text", "field_type": "column"},
            "cc_legal_name": {"data_type": "text", "field_type": "column"},
            "cc_organization_name": {"data_type": "text", "field_type": "column"},
            "cc_organization_ntee_codes": {"data_type": "text", "field_type": "column"},
            "cc_revocation_code": {"data_type": "text", "field_type": "column"},
            "cc_ruling_year": {"data_type": "text", "field_type": "column"},
            "cc_subsection_description": {"data_type": "text", "field_type": "column"},
            
            # OFAC fields
            "ofac_state": {"data_type": "text", "field_type": "column"},
            "org_ofac_result": {"data_type": "text", "field_type": "column"},
            "org_ofac_run_date": {"data_type": "text", "field_type": "column"},
            
            # Organization type fields
            "organization_type": {"data_type": "text", "field_type": "column"},
            "organization_mission": {"data_type": "text", "field_type": "column"},
            "chaptersig_standing_status": {"data_type": "text", "field_type": "column"},
            "chaptersig_status": {"data_type": "text", "field_type": "column"},
            "chaptersig_type": {"data_type": "text", "field_type": "column"},
            "chaptersigbadge": {"data_type": "text", "field_type": "column"},
            
            # Additional text fields
            "additional_internal_comments": {"data_type": "text", "field_type": "column"},
            "banking_beneficiary_name": {"data_type": "text", "field_type": "column"},
            "comments_to_applicant": {"data_type": "text", "field_type": "column"},
            "educational_roles": {"data_type": "text", "field_type": "column"},
            "educational_roles_1": {"data_type": "text", "field_type": "column"},
            "fdtn_country": {"data_type": "text", "field_type": "column"},
            "grantee_publish_contact_warning": {"data_type": "text", "field_type": "column"},
            "iso_country": {"data_type": "text", "field_type": "column"},
            "member_nova_id": {"data_type": "text", "field_type": "column"},
            "migrate_source_name": {"data_type": "text", "field_type": "column"},
            "org_bank_details": {"data_type": "text", "field_type": "column"},
            "org_bureau_region": {"data_type": "text", "field_type": "column"},
            "regional_bureau": {"data_type": "text", "field_type": "column"},
            "salesforce_oid": {"data_type": "text", "field_type": "column"},
            "state_1": {"data_type": "text", "field_type": "column"},
            "ext_sync_id": {"data_type": "text", "field_type": "column"},
            "grant_summary_table": {"data_type": "text", "field_type": "column"},
            "sort_as_name": {"data_type": "text", "field_type": "column"},
            
            # String computed fields
            "__advanced_filter": {"data_type": "text", "field_type": "string"},
            "__full_text": {"data_type": "text", "field_type": "string"},
            "__index_plan": {"data_type": "text", "field_type": "string"},
            "__managed_in_elastic_search": {"data_type": "text", "field_type": "string"},
            "__recent_records_tracking": {"data_type": "text", "field_type": "string"},
            "address_str": {"data_type": "text", "field_type": "string"},
            "all_notes": {"data_type": "text", "field_type": "string"},
            "c3_serialized_response": {"data_type": "text", "field_type": "string"},
            "city_str": {"data_type": "text", "field_type": "string"},
            "country_code": {"data_type": "text", "field_type": "string"},
            "country_name": {"data_type": "text", "field_type": "string"},
            "country_str": {"data_type": "text", "field_type": "string"},
            "filter_state": {"data_type": "text", "field_type": "string"},
            "geocode_response_full_address": {"data_type": "text", "field_type": "string"},
            "organization_id": {"data_type": "text", "field_type": "string"},
            "postal_code_str": {"data_type": "text", "field_type": "string"},
            "previous_state": {"data_type": "text", "field_type": "string"},
            "state_code": {"data_type": "text", "field_type": "string"},
            "state_description": {"data_type": "text", "field_type": "string"},
            "state_name": {"data_type": "text", "field_type": "string"},
            "state_str": {"data_type": "text", "field_type": "string"},
            "state_to_english": {"data_type": "text", "field_type": "string"},
            "sum_ofac_state": {"data_type": "text", "field_type": "string"},
            "to_s": {"data_type": "text", "field_type": "string"},
            
            # Relation fields - IDs
            "created_by_id": {"data_type": "bigint", "field_type": "relation"},
            "data_language_id": {"data_type": "bigint", "field_type": "relation"},
            "fx_type": {"data_type": "bigint", "field_type": "relation"},
            "geo_country_id": {"data_type": "bigint", "field_type": "relation"},
            "geo_county_id": {"data_type": "bigint", "field_type": "relation"},
            "geo_place_id": {"data_type": "bigint", "field_type": "relation"},
            "geo_state_id": {"data_type": "bigint", "field_type": "relation"},
            "model_theme_id": {"data_type": "bigint", "field_type": "relation"},
            "parent_org_id": {"data_type": "bigint", "field_type": "relation"},
            "primary_user_id": {"data_type": "bigint", "field_type": "relation"},
            "program": {"data_type": "bigint", "field_type": "relation"},
            "segment": {"data_type": "bigint", "field_type": "relation"},
            "segment_tag_id": {"data_type": "bigint", "field_type": "relation"},
            "translation_assignee": {"data_type": "bigint", "field_type": "relation"},
            "updated_by_id": {"data_type": "bigint", "field_type": "relation"},
            "zoom_concept_initiative": {"data_type": "bigint", "field_type": "relation"},
            
            # Relation fields - Arrays/JSON (keeping only _ids versions where available)
            "affiliate_type_list": {"data_type": "json", "field_type": "relation"},
            "alert_emails": {"data_type": "json", "field_type": "relation"},
            "alert_ids_sent": {"data_type": "json", "field_type": "relation"},
            "all_request_ids": {"data_type": "json", "field_type": "relation"},
            "any_etl_relationship_ids": {"data_type": "json", "field_type": "relation"},
            "audit_soft_deletes": {"data_type": "json", "field_type": "relation"},
            "bank_accounts": {"data_type": "json", "field_type": "relation"},
            "census_code_results": {"data_type": "json", "field_type": "relation"},
            "clone_ancestries": {"data_type": "json", "field_type": "relation"},
            "code_block_conversions": {"data_type": "json", "field_type": "relation"},
            "coi_ids": {"data_type": "json", "field_type": "relation"},
            "connection_ids": {"data_type": "json", "field_type": "relation"},
            "created_by": {"data_type": "bigint", "field_type": "relation"},
            "etl_relationships": {"data_type": "json", "field_type": "relation"},
            "etl_request_budget_ids": {"data_type": "json", "field_type": "relation"},
            "etl_request_transaction_budget_ids": {"data_type": "json", "field_type": "relation"},
            "favorite_user_ids": {"data_type": "json", "field_type": "relation"},
            "favorites": {"data_type": "json", "field_type": "relation"},
            "fiscal_requests": {"data_type": "json", "field_type": "relation"},
            "geo_place_relationships": {"data_type": "json", "field_type": "relation"},
            "grant_ids": {"data_type": "json", "field_type": "relation"},
            "grant_initiative_ids": {"data_type": "json", "field_type": "relation"},
            "grant_outcome_ids": {"data_type": "json", "field_type": "relation"},
            "grant_program_ids": {"data_type": "json", "field_type": "relation"},
            "grant_requests": {"data_type": "json", "field_type": "relation"},
            "grant_sub_initiative_ids": {"data_type": "json", "field_type": "relation"},
            "grant_sub_program_ids": {"data_type": "json", "field_type": "relation"},
            "group_ids": {"data_type": "json", "field_type": "relation"},
            "group_members": {"data_type": "json", "field_type": "relation"},
            "model_document_ids": {"data_type": "json", "field_type": "relation"},
            "model_emails": {"data_type": "json", "field_type": "relation"},
            "model_summaries": {"data_type": "json", "field_type": "relation"},
            "modifications": {"data_type": "json", "field_type": "relation"},
            "notes": {"data_type": "json", "field_type": "relation"},
            "ofac_people": {"data_type": "json", "field_type": "relation"},
            "organization_connection_requests": {"data_type": "json", "field_type": "relation"},
            "post_relationships": {"data_type": "json", "field_type": "relation"},
            "posts": {"data_type": "json", "field_type": "relation"},
            "primary_user_organization": {"data_type": "json", "field_type": "relation"},
            "project_ids_sql": {"data_type": "json", "field_type": "relation"},
            "project_organizations": {"data_type": "json", "field_type": "relation"},
            "projects": {"data_type": "json", "field_type": "relation"},
            "rd_tab_bank_accounts": {"data_type": "json", "field_type": "relation"},
            "rd_tab_cois": {"data_type": "json", "field_type": "relation"},
            "rd_tab_dyn_model_2": {"data_type": "json", "field_type": "relation"},
            "rd_tab_dyn_model_4": {"data_type": "json", "field_type": "relation"},
            "rd_tab_etl_relationships": {"data_type": "json", "field_type": "relation"},
            "rd_tab_grant_requests_grant": {"data_type": "json", "field_type": "relation"},
            "rd_tab_grant_requests_pri_grant": {"data_type": "json", "field_type": "relation"},
            "rd_tab_grant_requests_pri_request": {"data_type": "json", "field_type": "relation"},
            "rd_tab_grant_requests_request": {"data_type": "json", "field_type": "relation"},
            "rd_tab_grant_requests_request_or_grant": {"data_type": "json", "field_type": "relation"},
            "rd_tab_gs_streams": {"data_type": "json", "field_type": "relation"},
            "rd_tab_lois": {"data_type": "json", "field_type": "relation"},
            "rd_tab_model_documents": {"data_type": "json", "field_type": "relation"},
            "rd_tab_projects": {"data_type": "json", "field_type": "relation"},
            "rd_tab_request_regrants": {"data_type": "json", "field_type": "relation"},
            "rd_tab_request_reports": {"data_type": "json", "field_type": "relation"},
            "rd_tab_request_reviews": {"data_type": "json", "field_type": "relation"},
            "rd_tab_request_transactions": {"data_type": "json", "field_type": "relation"},
            "rd_tab_users": {"data_type": "json", "field_type": "relation"},
            "rd_tab_work_tasks": {"data_type": "json", "field_type": "relation"},
            "regrant_ids": {"data_type": "json", "field_type": "relation"},
            "related_bank_account_ids": {"data_type": "json", "field_type": "relation"},
            "related_coi_ids": {"data_type": "json", "field_type": "relation"},
            "related_granted_request_ids": {"data_type": "json", "field_type": "relation"},
            "related_gs_stream_ids": {"data_type": "json", "field_type": "relation"},
            "related_loi_ids": {"data_type": "json", "field_type": "relation"},
            "related_org_ids": {"data_type": "json", "field_type": "relation"},
            "related_request_ids": {"data_type": "json", "field_type": "relation"},
            "related_request_review_ids": {"data_type": "json", "field_type": "relation"},
            "related_user_ids": {"data_type": "json", "field_type": "relation"},
            "request_ids": {"data_type": "json", "field_type": "relation"},
            "request_organizations": {"data_type": "json", "field_type": "relation"},
            "request_regrants": {"data_type": "json", "field_type": "relation"},
            "request_report_ids": {"data_type": "json", "field_type": "relation"},
            "request_transaction_ids": {"data_type": "json", "field_type": "relation"},
            "reverse_parent_financial_audit_organization_MacModelTypeDynFinancialAudit": {"data_type": "json", "field_type": "relation"},
            "reverse_project_sponsor_GrantRequest": {"data_type": "json", "field_type": "relation"},
            "reverse_undelete_org_MacModelTypeDynTool": {"data_type": "json", "field_type": "relation"},
            "satellite_org_ids": {"data_type": "json", "field_type": "relation"},
            "tag_ids": {"data_type": "json", "field_type": "relation"},
            "taggings": {"data_type": "json", "field_type": "relation"},
            "translator_assignments": {"data_type": "json", "field_type": "relation"},
            "user_ids": {"data_type": "json", "field_type": "relation"},
            "user_organizations": {"data_type": "json", "field_type": "relation"},
            "work_tasks": {"data_type": "json", "field_type": "relation"},
            "workflow_events": {"data_type": "json", "field_type": "relation"},
        },
    },
}


@dlt.source(name="fluxx", max_table_nesting=0)
def fluxx_source(
    instance: str = dlt.config.value,
    client_id: str = dlt.secrets.value,
    client_secret: str = dlt.secrets.value,
    start_date: Optional[pendulum.DateTime] = None,
    end_date: Optional[pendulum.DateTime] = None,
    resources: Optional[list] = None,
) -> Iterable[DltResource]:
    """
    Returns a list of resources to load data from Fluxx Grant Management API.

    Args:
        instance: The Fluxx instance subdomain (e.g., "isocfoundation.preprod")
        client_id: OAuth client ID
        client_secret: OAuth client secret
        start_date: Start date for incremental loading
        end_date: End date for incremental loading
        resources: List of resource names to load (defaults to all available)

    Returns:
        Iterable[DltResource]: Selected Fluxx resources
    """

    # Get OAuth access token
    access_token = get_access_token(instance, client_id, client_secret)

    # If no resources specified, load all available
    if resources is None:
        resources = list(FLUXX_RESOURCES.keys())

    # Create dynamic resources based on configuration
    resource_list = []
    for resource_name in resources:
        if resource_name not in FLUXX_RESOURCES:
            continue

        config = FLUXX_RESOURCES[resource_name]
        resource = create_dynamic_resource(
            resource_name=resource_name,
            endpoint=config["endpoint"],
            instance=instance,
            access_token=access_token,
            start_date=start_date,
            end_date=end_date,
            fields_to_extract=config["fields"],
        )
        resource_list.append(resource)

    return resource_list
