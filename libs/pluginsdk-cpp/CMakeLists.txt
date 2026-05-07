cmake_minimum_required(VERSION 3.16)
project(notbbg-pluginsdk-cpp CXX)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_CXX_EXTENSIONS OFF)

add_library(notbbg-pluginsdk STATIC
    src/pluginsdk.cpp
)
target_include_directories(notbbg-pluginsdk PUBLIC include)
target_compile_options(notbbg-pluginsdk PRIVATE -Wall -Wextra -Wpedantic)

# Tests — built when -DNOTBBG_PLUGINSDK_TESTS=ON.
option(NOTBBG_PLUGINSDK_TESTS "Build pluginsdk-cpp tests" OFF)
if(NOTBBG_PLUGINSDK_TESTS)
    enable_testing()
    add_executable(pluginsdk_test tests/pluginsdk_test.cpp)
    target_link_libraries(pluginsdk_test PRIVATE notbbg-pluginsdk)
    add_test(NAME pluginsdk_test COMMAND pluginsdk_test)
endif()
