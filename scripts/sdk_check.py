from openai import OpenAI

client = OpenAI(
    base_url="http://localhost:8080/v1",
    api_key="not-the-real-key",  # the gateway replaces this key
    max_retries=0,               # show each error at once, do not retry
)
model = "gpt-4o-mini"

# 1. A normal response
r = client.chat.completions.create(
    model=model,
    messages=[{"role": "user", "content": "Say hello"}],
)
print("non-stream:", r.choices[0].message.content)

# 2. A stream
s = client.chat.completions.create(
    model=model,
    messages=[{"role": "user", "content": "Say hello"}],
    stream=True,
)
print("stream:    ", end="")
for chunk in s:
    if chunk.choices and chunk.choices[0].delta.content:
        print(chunk.choices[0].delta.content, end="", flush=True)
print()

# 3. Content as an array (vision models and many tools send this format)
r = client.chat.completions.create(
    model=model,
    messages=[{"role": "user", "content": [{"type": "text", "text": "Say hello"}]}],
)
print("array:", r.choices[0].message.content)
