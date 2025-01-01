

import requests


def custom_table(access_token: str, account_id: list[str], start_date: str, end_date: str, dimensions: list[str], metrics: list[str],
                 time_granularity: str = "DAILY", pivot: str = "CAMPAIGN", fields: str = "pivotValues,dateRange"
                 ) -> dict:
   
    base_url = "https://api.linkedin.com/rest/adAnalytics"
    
    params = {
        "q": "analytics",
        "timeGranularity": time_granularity,
        "dateRange": f"(start:(year:{start_date['year']},month:{start_date['month']},day:{start_date['day']}))",
        "accounts": f"List(urn:li:sponsoredAccount:{account_id})",
        "pivot": pivot,
        "fields": fields
    }
    
    headers = {
        "Authorization": f"Bearer {access_token}",
        "Linkedin-Version": "202411",
        "X-Restli-Protocol-Version": "2.0.0"
    }
    
    response = requests.get(base_url, params=params, headers=headers)
    response.raise_for_status()
    
    return response.json()



