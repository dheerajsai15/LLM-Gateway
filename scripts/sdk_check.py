import sys

import openai
from openai import OpenAI

client = OpenAI(
    base_url="http://localhost:8080/v1",
    api_key="not-the-real-key",  # the gateway replaces this key
    max_retries=0,               # show each error at once, do not retry
)
# The model comes from the command line, for example claude-haiku-5-5.
model = sys.argv[1] if len(sys.argv) > 1 else "gpt-4o-mini"
failed = 0


def check(name, fn):
    """Run one check and print its result. An SDK error fails only this check."""
    global failed
    try:
        print(f"{name}: {fn()}")
    except openai.APIError as e:
        failed += 1
        print(f"{name}: FAIL {type(e).__name__}: {e}")


# 1. A normal response
def non_stream():
    r = client.chat.completions.create(
        model=model,
        messages=[{"role": "user", "content": "Say hello"}],
    )
    return r.choices[0].message.content


# 2. A stream
def stream():
    s = client.chat.completions.create(
        model=model,
        messages=[{"role": "user", "content": "Say hello"}],
        stream=True,
        stream_options={"include_usage": True},
    )
    text = ""
    usage = None
    for chunk in s:
        if chunk.choices and chunk.choices[0].delta.content:
            text += chunk.choices[0].delta.content
        # The usage chunk has no choices. It comes last, before [DONE].
        if chunk.usage:
            usage = chunk.usage
    if usage is None:
        return f"{text} [no usage chunk]"
    return f"{text} [tokens: prompt={usage.prompt_tokens} completion={usage.completion_tokens} total={usage.total_tokens}]"


# 3. Content as an array (vision models and many tools send this format)
def array_content():
    r = client.chat.completions.create(
        model=model,
        messages=[{"role": "user", "content": [{"type": "text", "text": "Say hello"}]}],
    )
    return r.choices[0].message.content


check("non-stream", non_stream)
check("stream", stream)
check("array", array_content)
sys.exit(1 if failed else 0)
