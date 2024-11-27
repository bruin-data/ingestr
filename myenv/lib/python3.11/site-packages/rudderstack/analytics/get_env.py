import os
from dotenv import load_dotenv

load_dotenv()

TEST_WRITE_KEY = os.getenv('TEST_WRITE_KEY')
TEST_DATA_PLANE_URL = os.getenv('TEST_DATA_PLANE_URL')
