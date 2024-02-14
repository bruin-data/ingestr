# Where should we write the GIF?
Output demo.gif

# Set up a 1200x600 terminal with 46px font.
Set FontSize 20
Set Width 1200
Set Height 900
Set LineHeight 1.8
Set TypingSpeed 0.01
Set Framerate 60

Type "ingestr ingest \"
Ctrl+Enter

Type "   --source-uri $POSTGRES_URI \"
Ctrl+Enter

Type "   --source-table 'testschema.table_name' \"
Ctrl+Enter

Type "   --dest-uri $MSSQL_URI\"
Ctrl+Enter

Type "   --dest-table 'testschema.table_name'"
Enter

Sleep 2s

Type "y"
Ctrl+Enter

Sleep 1s