from hubspot.helpers import chunk_properties


def test_chunk_properties():
    props = ["hs_object_id", "name", "description", "createdate", "lastmodifieddate"]
    chunks = chunk_properties(props)
    assert len(chunks) == 1
    assert chunks[0] == ','.join(props)
    
def test_chunk_properties_with_max_length():
    props = ["hs_object_id", "name", "description", "createdate", "lastmodifieddate"]
    chunks = chunk_properties(props, max_length=30)
    assert len(chunks) == 3
    assert chunks[0] == 'hs_object_id,name,description'
    assert chunks[1] == 'hs_object_id,createdate'
    assert chunks[2] == 'hs_object_id,lastmodifieddate'
    
