Instead of hardcoding the backends, make the backend system pluggable where i can plug in different backends, multiple backends and models can be running at the same time. Prefer ipc, stdio communication with the backend over http port, if that is supported.

I should be able to download standard LLM backends.

The application should run on docker as well as native on windows, linux.

Add an API for working with the application.

Add a UI for working with the application
- UI should allow downloading LLM models from huggingface
- UI should allow downloading standard backends
- Allow configuring model, select backed to use and changing common parameters for each model
- UI should allow running an LLM model or stopping a model.
- 



create a web application that allows installing and running multiple llm models, installing multiple backends, provides an openai compatible API endpoint to talk to the LLM model, pick a good framework that can handle high traffic, lightweight and can handle large number of concurrent clients