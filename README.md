AI browser agent that can navigate websites by following a natural language instruction (for example: *“order 2 pizzas ”*).

How it works:

* The agent receives a text task and starts a step-by-step execution loop.
* On each step, the browser takes a **snapshot**: URL, title, AX tree, a map of interactive elements, and a screenshot of the page.
* The snapshot and step history are sent to the LLM client, which decides what to do next: click, type, scroll, or finish the task.
* The agent executes the chosen action via the **Chrome DevTools Protocol** (chromedp), safely clicking buttons, radio buttons, checkboxes, and then takes another snapshot.
* At the end, it generates a **report**: the sequence of steps and a short summary (for example, what was added to the cart and which screen the scenario ended on).

Tech stack:

* Go
* chromedp (CDP)
* OpenAI-compatible LLM client
* AX tree for robust detection of interactive elements

Demo video of agent in action:
[https://youtu.be/E6QdEMtpCGI](https://youtu.be/E6QdEMtpCGI)
