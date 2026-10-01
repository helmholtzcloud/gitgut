# GitGut

> [!CAUTION]
> Like many projects in the LLM space, this software is at an experimental stage. No guarantees, e.g. it may burn a lot of tokens. Use at your own risk.

Welcome to GitGut, a customizable LLM assistant for code review! It works like this: point the CLI tool at a MR/PR on your forge, then it is going execute an LLM-based review workflow for you and report its findings in a comment it updates on each run.

The workflow is made up of a set of blocks provided by GitGut which you customize to fit your team's needs using custom workflow layout, models, model parameters, prompts etc. Currently, GitGut is not a 'traditional' agent, e.g. the 'agentic' validation block only has a single tool and that is `read_file` which is read from an im-memory copy of your repo. This may be change in the future.

GitGut is designed as a tool augmenting human code review, not for replacing it. It is supposed to help you and your team uncover issues not covered by linters (e.g. architecture decisions or consistent wording) and help you improve your craft as a developer, another set of eyes, another gut-check so to say.

## API / Platform Support
- Forges
    - GitLab
- Inference API
    - OpenAI Compatible Endpoint (Completions)

*There are provisions in place to support other forges and inference backends, please open an issue in that case.*

## Quickstart
- Grab a `gitgut.json` from the section below: a short single-node review workflow or copy the larger example workflow (which may use quite a few tokens)
- Update the config (forge + inference credentials + models you want to use)
- Dry-run it in a container:
```
podman run --rm -it -v ./gitgut.json:/gitgut.json:ro ghcr.io/helmholtzcloud/gitgut:latest gitgut review 123 456 --dry-run
```
- Iterate on the results a few times and once you're happy, you can deploy it in CI etc.

Use the `help` command to learn about more options.

## Limitations
- Extremely large MRs which are larger than the context may cause touble (probably too large for human review anyway)
- Repos which contain excessively large files and fill up the server's RAM when the repo's tar.gz is extracted in memory
- In case you use the 'agentic' validation action block:
    - Repos with a lot of files which fill the context with the file tree alone
    - Sometimes agentic verification gets stuck in a loop (depends on model, prompt etc.) leading to excessive token consumption
- There is a hardcoded limit of 10 million input tokens per run (measured after a node finishes, so the actual number may be higher). This is a stopgap for runaway jobs and will be customizable in the future.
- There is a default timeout on many actions to prevent stuck tasks.

## Building your own Workflow
The answer to what part of code review can be supported by LLMs is probably going to slightly differ for each team. Therefore, you can customize the workflow GitGut is executing to fit it to your needs. It is recommended to pick a few typical benchmark MRs and iteratively build your workflow with `--dry-run` locally first! Later on, you're free to keep running in local mode or deploy it as part of your CI pipeline.

GitGut workflows are made up of customizable action blocks. Workflows generate review findings, pipe them through a bunch of filters and validation and finally compose the review comment. The general principle is as follows:

- There are source blocks for extracting review findings from code/diffs and any existing comment posted by GitGut
- Intermediate blocks take a list of findings and perform an action on each of them
- The comment generation block is the final block in the workflow

## CLI

Config is loaded from `gitgut.json`.

```
GitGut is a customizable code review tool for LLM-assisted workflows

Usage:
  gitgut [command]

Available Commands:
  completion  Generate the autocompletion script for the specified shell
  help        Help about any command
  licenses    Display license information of this project.
  review      Run a code review and post/update the comment.
  version     Display version information of this build

Flags:
  -h, --help   help for gitgut

Use "gitgut [command] --help" for more information about a command.
```

## Configuration Reference

This is the configuration reference of `gitgut.json`, check below for a full example. If you want to interpolate credentials please use envsubst.

### General

#### Forge Config

##### GitLab

You need a credential which can download the code, read merge requests and post comments on the project you want to review, be sure to protect your branches and use the least privileged token you can generate (reporter, you might need developer on self-hosted).

You can provide a list of user IDs who have opted into reviews on their MR. If you want to disable this feature, simply set the attribute to `null`.

Review comments are posted as regular, externally-visible notes by default, so anyone with access to the merge request can see them. Set `post_as_internal_note` to `true` if you want them restricted to project members only.

```json
"forge": {
    "client": "gitlab",
    "host": "my.gitlab.local",
    "access_token": "GITGUT_GITLAB_PAT",
    "opted_in_user_ids": [
        1234
    ],
    "post_as_internal_note": false
}
```

*There are provisions in place to support other forges and inference backends, please open an issue in that case.*

#### Inference Provider

##### OpenAI Compatible Completions Endpoint

OpenAI compatible endpoint + API key, a run can consume a few million input tokens, a token caching backend is advantageous!

```json
"providers": {
    "local_provider": {
        "client": "openai",
        "url": "https://myprovider.local/api/",
        "key": "GITGUT_INFERENCE_KEY"
    }
}
```

*There are provisions in place to support other forges and inference backends, please open an issue in that case.*

#### Models

In the models section you can define multiple models you can use in your blocks. You can also add the same model multiple times, just with different settings such as thinking effort or constraining it to structured output.

```json
"thinking": {
    "provider": "my_local_provider",
    "name": "Model name at provider",
    "additional_params": {
        "enable_thinking": true,
        "max_tokens": 100000,
        "custom_parameter": "yes"
    }
}
```

### Workflow

The general workflow settings look like this:

```json
"system_prompt": "You are GitGut - a friendly yet professional code review bot. Be enthusiastic, wise and encouraging. Gently help developers grow and improve their craft.",
"guideline_paths": [
    "README.md"
],
```

#### system_prompt

The prompt for setting the writing style of the review text.

#### guideline_paths

Paths to files which are going to be auto-included in the review context.

### Workflow Action Blocks

> [!TIP]
> Please pay attention when building your workflow. There are a few sanity checks on the referential integrity within a workflow config, however as-is there are no automated checks on whether you built a loop or your start with source blocks and end it with the comment block.

Quick rundown of the generic action parameters:

- `action`: the action block to run
- `inputs_from`: for actions that use results from other actions, define the dependencies

#### Source Blocks

Source action blocks source findings from the code or GitGut's existing review comment.

##### extract_findings_from_comment

Take the current GitGut comment's text (if any) and extract any review findings contained therein.
You probably want to include this action in each workflow and combine it with a deduplication and verification step if you do not want to 're-discover' findings by chance on each review.

```json
"load_comment": {
    "action": "extract_findings_from_comment",
    "model": "simple_model"
}
```

##### review

Conduct a one-shot review and return any findings. The LLM sees the diff, the full contents of the files in the diff's target rev and any auto-included files you defined in the config (e.g. a README outlining the project's architecture).
If the variance if findings returned is too large, you might want to use a `multi_review`.

```json
"review_texts": {
    "action": "review",
    "model": "thinking_max_model",
    "prompt": "Review the prose in the UI for grammatical errors and consistent wording."
}
```

##### multi_review

Conduct `reviews` number of parallel reviews of the change and return those findings which have been found at least `quorum` number of times. This is fairly expensive from a token use perspective (only one is run in parallel though to enhance the probability of using token caches), but you can use this to filter for the more obvious findings and skipping over nitpicks. Note that counting/aggregation is also done by an LLM and may be inaccurate. The LLM sees the diff, the full contents of the files in the diff's target rev and any auto-included files you defined in the config (e.g. a README outlining the project's architecture).

```json
"architecture_review": {
    "action": "multi_review",
    "review_model": "thinking_max_model",
    "aggregation_model": "thinking_max_model",
    "reviews": 3,
    "quorum": 2,
    "prompt": "Please review the architectural impact of the change and extract your findings. It is okay to not find any issues. When discussing findings put them in context and also explain the long-term impact of design decisions made now in the description of each finding."
}
```

#### Intermediate Blocks

Intermediate blocks filter / aggregate findings reported by source blocks or other intermediate blocks.

##### filter_findings

Filter findings by arbitrary conditions defined in the prompt. The LLM only sees the list of findings, no further context.

```json
"filter_actionable": {
    "action": "filter_findings",
    "input_from": [
        "review"
    ],
    "model": "thinking_model",
    "prompt": "Select code review findings which require actions to be taken to be resolved. Ignore all general observations, or findings which do not have any impact."
}
```

##### verify_findings

> [!WARNING]
> Of all the action blocks this is probably the most experimental one. Depending on the model it may get stuck in loops or request to read fiels that do not exist etc. Be sure to set `max_tokens` on the model referenced by this block or other similar parameters to avoid endlessly burning through tokens.

Filter the list of findings for findings which can be proven to exist in the code (adds the verification notes to finding's descriptions). The LLM sees the diff, the full contents of the files in the diff's target rev and any auto-included files you defined in the config (e.g. a README outlining the project's architecture). This is a somewhat agentic workflow where the agent can pull entire files from the target ref into the context as needed.

```json
"verify": {
    "action": "verify_findings",
    "input_from": [
        "filter_actionable"
    ],
    "model": "thinking_model",
    "prompt": "The above finding was reported during code review, your task is to verify whether this finding is valid by inspecting the codebase: There is a high chance the finding is outdated or lists a hypothetical concern. The finding is valid ONLY IF you can prove the location where a change causes an issue as-is."
}
```

#### Comment Block

##### generate_comment

Concatenate the AI notice, a generate a summary and the findings from `input_from`. This is going to be posted as the review comment.

```json
"generate_comment": {
    "action": "generate_comment",
    "input_from": [
        "verify"
    ],
    "summary_model": "model",
    "summary_prompt": "Provide the developer with a short summary of your code review's results (about 1 paragraph max, can be shorter) without listing each individual item, so the developer working on the change knows where to start. Also state whether you think the changes risk breaking major things when merging."
}
```

### Example gitgut.json

#### Simple Review

This is an example workflow which aims to provide general findings on the code.

```json
{
    "forge": {
        "client": "gitlab",
        "host": "my.gitlab.local",
        "access_token": "GITGUT_GITLAB_PAT",
        "opted_in_user_ids": [1234]
    },
    "providers": {
        "my_local_provider": {
            "client": "openai",
            "url": "https://myprovider.local/api/",
            "key": "GITGUT_INFERENCE_KEY"
        }
    },
    "models": {
        "model": {
            "provider": "my_local_provider",
            "name": "model_name_at_provider",
            "additional_params": {
                "max_tokens": 256000,
                "max_completion_tokens": 100000
            }
        },
        "model_thinking": {
            "provider": "my_local_provider",
            "name": "model_name_at_provider",
            "additional_params": {
                "enable_thinking": true,
                "reasoning_effort": "max",
                "max_tokens": 256000,
                "max_completion_tokens": 100000
            }
        },
        "model_thinking_json": {
            "provider": "my_local_provider",
            "name": "model_name_at_provider",
            "additional_params": {
                "enable_thinking": true,
                "reasoning_effort": "max",
                "response_format": {
                    "type": "json_object"
                },
                "max_tokens": 256000,
                "max_completion_tokens": 100000
            }
        }
    },
    "review": {
        "system_prompt": "You are GitGut - a friendly yet professional code review bot. Be enthusiastic, wise and encouraging. Gently help developers grow and improve their craft.",
        "guideline_paths": [
            "README.md"
        ],
        "workflow": {
            "extract": {
                "action": "extract_findings_from_comment",
                "model": "model_thinking_json"
            },
            "architecture_review": {
                "action": "review",
                "review_model": "model_thinking_json",
                "prompt": "Please review the architectural impact of the change and extract your findings. It is okay to not find any issues. When discussing findings put them in context and also explain the long-term impact of design decisions made now in the description of each finding."
            },
            "deduplicate": {
                "action": "filter_findings",
                "input_from": [
                    "extract",
                    "architecture_review"
                ],
                "model": "model_thinking_json",
                "prompt": "Deduplicate the list of findings. Return only the first one for each duplicate."
            },
            "generate_comment": {
                "action": "generate_comment",
                "input_from": [
                    "deduplicate"
                ],
                "summary_model": "model_thinking",
                "summary_prompt": "Provide the developer with a short summary of your code review's results (about 1 paragraph max, can be shorter) without listing each individual item, so the developer working on the change knows where to start. Also state whether you think the changes risk breaking major things when merging."
            }
        }
    }
}
```

#### Architecture Review

This is an example workflow which aims to provide general verified findings on the code.
Please be aware this may use a few million tokens per review if your provider does not support caching.

```json
{
    "forge": {
        "client": "gitlab",
        "host": "my.gitlab.local",
        "access_token": "GITGUT_GITLAB_PAT",
        "opted_in_user_ids": [1234]
    },
    "providers": {
        "my_local_provider": {
            "client": "openai",
            "url": "https://myprovider.local/api/",
            "key": "GITGUT_INFERENCE_KEY"
        }
    },
    "models": {
        "model": {
            "provider": "my_local_provider",
            "name": "model_name_at_provider",
            "additional_params": {
                "max_tokens": 256000,
                "max_completion_tokens": 100000
            }
        },
        "model_thinking": {
            "provider": "my_local_provider",
            "name": "model_name_at_provider",
            "additional_params": {
                "enable_thinking": true,
                "reasoning_effort": "max",
                "max_tokens": 256000,
                "max_completion_tokens": 100000
            }
        },
        "model_thinking_json": {
            "provider": "my_local_provider",
            "name": "model_name_at_provider",
            "additional_params": {
                "enable_thinking": true,
                "reasoning_effort": "max",
                "response_format": {
                    "type": "json_object"
                },
                "max_tokens": 256000,
                "max_completion_tokens": 100000
            }
        }
    },
    "review": {
        "system_prompt": "You are GitGut - a friendly yet professional code review bot. Be enthusiastic, wise and encouraging. Gently help developers grow and improve their craft.",
        "guideline_paths": [
            "README.md"
        ],
        "workflow": {
            "extract": {
                "action": "extract_findings_from_comment",
                "model": "model_thinking_json"
            },
            "architecture_review": {
                "action": "multi_review",
                "review_model": "model_thinking_json",
                "aggregation_model": "model_thinking_json",
                "reviews": 3,
                "quorum": 2,
                "prompt": "Please review the architectural impact of the change and extract your findings. It is okay to not find any issues. When discussing findings put them in context and also explain the long-term impact of design decisions made now in the description of each finding."
            },
            "deduplicate": {
                "action": "filter_findings",
                "model": "model_thinking_json",
                "prompt": "Deduplicate the list of findings. Return only the first one for each duplicate.",
                "input_from": [
                    "extract",
                    "architecture_review"
                ]
            },
            "filter_actionable": {
                "action": "filter_findings",
                "model": "model_thinking_json",
                "prompt": "Select code review findings which require actions to be taken to be resolved. Ignore all general observations, or findings which do not have any impact.",
                "input_from": [
                    "deduplicate"
                ]
            },
            "verify": {
                "action": "verify_findings",
                "prompt": "The above finding was reported during code review, your task is to verify whether this finding is valid by inspecting the codebase: There is a high chance the finding is outdated or lists a hypothetical concern. The finding is valid ONLY IF you can prove the location where a change causes an issue as-is.",
                "model": "model_thinking",
                "input_from": [
                    "filter_actionable"
                ]
            },
            "generate_comment": {
                "action": "generate_comment",
                "input_from": [
                    "verify"
                ],
                "summary_model": "model_thinking",
                "summary_prompt": "Provide the developer with a short summary of your code review's results (about 1 paragraph max, can be shorter) without listing each individual item, so the developer working on the change knows where to start. Also state whether you think the changes risk breaking major things when merging."
            }
        }
    }
}
```

## Origin of the Name

I mis-typed gitgud, gut means means good in German and in a way this also provides another gut check for your code changes, so there you go :)
