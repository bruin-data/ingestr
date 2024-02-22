Output demo.gif

Set FontSize 16
Set Width 1250
Set Height 600
Set LineHeight 1.5
Set TypingSpeed 0.01
Set Framerate 60

Type "ingestr ingest --source-uri $POSTGRES_URI --source-table 'testschema.table_name' --dest-uri $BIGQUERY_URI"
Enter

Sleep 2s

Type "y"
Enter

Sleep 15s