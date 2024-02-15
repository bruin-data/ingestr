Output demo.gif

Set FontSize 16
Set Width 1200
Set Height 1200
Set LineHeight 1.8
Set TypingSpeed 0.01
Set Framerate 60

Type "ingestr ingest --source-uri $POSTGRES_URI --source-table 'testschema.table_name' --dest-uri $MSSQL_URI"
Enter

Sleep 2s

Type "y"
Enter

Sleep 15s