PROMPT='%F{blue}~/infra%f %F{magenta}❯%f '
autoload -U compinit && compinit -u
source <(kubectl completion zsh)
zstyle ':completion:*' list-separator '—'
demo_dir=${${(%):-%x}:a:h}
export KUBE_EDITOR="vim -u $demo_dir/vimrc"
