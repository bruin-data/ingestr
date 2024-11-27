//
// Copyright (c) 2012-2023 Snowflake Computing Inc. All rights reserved.
//

#include "SnowflakeType.hpp"

namespace sf {

std::unordered_map<std::string, SnowflakeType::Type>
    SnowflakeType::m_strEnumIndex = {
        {"ANY", SnowflakeType::Type::ANY},
        {"ARRAY", SnowflakeType::Type::ARRAY},
        {"BINARY", SnowflakeType::Type::BINARY},
        {"BOOLEAN", SnowflakeType::Type::BOOLEAN},
        {"CHAR", SnowflakeType::Type::CHAR},
        {"DATE", SnowflakeType::Type::DATE},
        {"DOUBLE PRECISION", SnowflakeType::Type::REAL},
        {"DOUBLE", SnowflakeType::Type::REAL},
        {"FIXED", SnowflakeType::Type::FIXED},
        {"FLOAT", SnowflakeType::Type::REAL},
        {"MAP", SnowflakeType::Type::MAP},
        {"OBJECT", SnowflakeType::Type::OBJECT},
        {"REAL", SnowflakeType::Type::REAL},
        {"STRING", SnowflakeType::Type::TEXT},
        {"TEXT", SnowflakeType::Type::TEXT},
        {"TIME", SnowflakeType::Type::TIME},
        {"TIMESTAMP", SnowflakeType::Type::TIMESTAMP},
        {"TIMESTAMP_LTZ", SnowflakeType::Type::TIMESTAMP_LTZ},
        {"TIMESTAMP_NTZ", SnowflakeType::Type::TIMESTAMP_NTZ},
        {"TIMESTAMP_TZ", SnowflakeType::Type::TIMESTAMP_TZ},
        {"VARCHAR", SnowflakeType::Type::TEXT},
        {"VARIANT", SnowflakeType::Type::VARIANT},
        {"VECTOR", SnowflakeType::Type::VECTOR}};

}  // namespace sf
