/** Parse quotes and escapes into literal argv, without shell expansion or execution. */
export function parseCommandOverride(text: string): string[] | undefined {
  const args: string[] = [];
  let argument = "";
  let started = false;
  let quote: "'" | '"' | null = null;

  for (let index = 0; index < text.length; index++) {
    const character = text[index];
    if (quote === "'") {
      if (character === "'") quote = null;
      else argument += character;
      continue;
    }
    if (character === "\\") {
      const next = text[++index];
      if (next === undefined) throw new Error("Incomplete escape in command override");
      if (next === "\n") continue;
      if (quote === '"' && !['"', "\\", "$", "`"].includes(next)) argument += "\\";
      argument += next;
      started = true;
      continue;
    }
    if (quote === '"') {
      if (character === '"') quote = null;
      else argument += character;
      continue;
    }
    if (character === "'" || character === '"') {
      quote = character;
      started = true;
    } else if (/\s/.test(character)) {
      if (started) args.push(argument);
      argument = "";
      started = false;
    } else {
      argument += character;
      started = true;
    }
  }

  if (quote)
    throw new Error(
      `Unterminated ${quote === "'" ? "single" : "double"} quote in command override`,
    );
  if (started) args.push(argument);
  return args.length > 0 ? args : undefined;
}
