// Field allow-lists for the Fakturoid v3 API.
//
// These are ALLOW-LISTS by design. Fakturoid returns fields beyond the ones projected
// here (attachments, eet_records, legacy_bank_details, vat_rates_summary on the invoice
// itself, ...); emitting everything would make the destination shape follow whatever the
// vendor adds next. Fields the API returns but that are absent here are counted and
// logged once per run as drift, never silently dropped.

package fakturoid

// invoiceFields: 84 columns — fields.json['invoices'] MINUS `lines`, which is exploded into invoices_lines.
var invoiceFields = []string{
	"bank_account", "cancelled_at", "client_city", "client_country",
	"client_delivery_city", "client_delivery_country", "client_delivery_name", "client_delivery_street",
	"client_delivery_zip", "client_has_delivery_address", "client_local_vat_no", "client_name",
	"client_registration_no", "client_street", "client_vat_no", "client_zip",
	"correction_id", "created_at", "currency", "custom_id",
	"custom_payment_method", "document_type", "due", "due_on",
	"exchange_rate", "footer_note", "generator_id", "gopay",
	"hide_bank_account", "html_url", "iban", "iban_visibility",
	"id", "issued_on", "language", "last_reminder_sent_at",
	"locked_at", "native_subtotal", "native_total", "note",
	"number", "number_format_id", "order_number", "oss",
	"paid_on", "payment_method", "paypal", "pdf_url",
	"private_note", "proforma_followup_document", "public_html_url", "related_id",
	"remaining_amount", "remaining_native_amount", "reminder_sent_at", "round_total",
	"rounding_adjustment", "sent_at", "show_already_paid_note_in_pdf", "status",
	"subject_custom_id", "subject_id", "subject_url", "subtotal",
	"supply_code", "swift_bic", "taxable_fulfillment_due", "token",
	"total", "transferred_tax_liability", "uncollectible_at", "updated_at",
	"url", "variable_symbol", "vat_price_mode", "webinvoice_seen_on",
	"your_city", "your_country", "your_local_vat_no", "your_name",
	"your_registration_no", "your_street", "your_vat_no", "your_zip",
}

// subjectFields: 49 columns — fields.json['subjects'] verbatim.
var subjectFields = []string{
	"ares_update", "bank_account", "city", "country",
	"created_at", "currency", "custom_email_text", "custom_estimate_email_text",
	"custom_id", "delivery_city", "delivery_country", "delivery_name",
	"delivery_street", "delivery_zip", "due", "email",
	"email_copy", "full_name", "has_delivery_address", "html_url",
	"iban", "id", "invoice_from_proforma_email_text", "language",
	"legal_form", "local_vat_no", "name", "overdue_email_text",
	"phone", "private_note", "registration_no", "setting_estimate_pdf_attachments",
	"setting_invoice_pdf_attachments", "setting_invoice_send_reminders", "setting_update_from_ares", "street",
	"suggestion_enabled", "swift_bic", "thank_you_email_text", "type",
	"unreliable", "unreliable_checked_at", "updated_at", "url",
	"variable_symbol", "vat_no", "web", "webinvoice_history",
	"zip",
}

// lineFields: 13 columns from invoice.lines[]; invoice_id is added by the reader.
var lineFields = []string{
	"id", "name", "quantity", "unit_name",
	"unit_price", "vat_rate", "unit_price_without_vat", "unit_price_with_vat",
	"total_price_without_vat", "total_vat", "native_total_price_without_vat", "native_total_vat",
	"inventory",
}

// vatRateFields: 8 columns from invoice.vat_rates_summary[]; invoice_id is added by the reader.
var vatRateFields = []string{
	"id", "vat_rate", "base", "vat",
	"currency", "native_base", "native_vat", "native_currency",
}
