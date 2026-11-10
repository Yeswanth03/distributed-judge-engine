package com.minicodejudge.catalog.service;

import com.minicodejudge.catalog.dto.ProblemDtos.FunctionDetails;
import com.minicodejudge.catalog.dto.ProblemDtos.FunctionParameter;
import com.minicodejudge.catalog.dto.TestsetDtos.CaseRequest;
import java.util.HashSet;
import java.util.Iterator;
import java.util.List;
import java.util.Set;
import java.util.regex.Pattern;
import tools.jackson.core.JacksonException;
import tools.jackson.databind.DeserializationFeature;
import tools.jackson.databind.JsonNode;
import tools.jackson.databind.json.JsonMapper;
import tools.jackson.databind.node.ObjectNode;

/** Rules for function details and for test cases written as JSON against them. */
final class FunctionRules {

    /** "self" is reserved too: the judge calls a method on class Solution, so a parameter cannot reuse that name. */
    private static final Set<String> RESERVED = Set.of(
            "False", "None", "True", "and", "as", "assert", "async", "await", "break", "class", "continue", "def",
            "del", "elif", "else", "except", "finally", "for", "from", "global", "if", "import", "in", "is", "lambda",
            "nonlocal", "not", "or", "pass", "raise", "return", "self", "try", "while", "with", "yield");

    private static final Pattern SOLUTION_CLASS = Pattern.compile("\\bclass\\s+Solution\\b");

    private static final JsonMapper STRICT = JsonMapper.builder()
            .enable(DeserializationFeature.FAIL_ON_TRAILING_TOKENS, DeserializationFeature.FAIL_ON_READING_DUP_TREE_KEY)
            .build();

    private FunctionRules() {}

    /** The rules bean validation cannot express: reserved and repeated names, and starter code matching the name. */
    static void checkDetails(FunctionDetails details) {
        if (RESERVED.contains(details.functionName())) {
            throw new UnprocessableException("function_name must not be the reserved word '" + details.functionName() + "'");
        }
        Set<String> seen = new HashSet<>();
        for (FunctionParameter p : details.parameters()) {
            if (RESERVED.contains(p.name())) {
                throw new UnprocessableException("parameters must not use the reserved word '" + p.name() + "'");
            }
            if (!seen.add(p.name())) {
                throw new UnprocessableException("parameters must not repeat the name '" + p.name() + "'");
            }
        }
        if (!SOLUTION_CLASS.matcher(details.starterCode()).find()) {
            throw new UnprocessableException("starter_code must define class Solution");
        }
        // functionName matched the identifier pattern, so it is safe inside this regex.
        if (!Pattern.compile("\\bdef\\s+" + details.functionName() + "\\s*\\(").matcher(details.starterCode()).find()) {
            throw new UnprocessableException("starter_code must define " + details.functionName());
        }
    }

    /**
     * Checks a case against the function and returns it in compact form with arguments in parameter order, so the
     * same case always stores, and hashes, the same text. {@code position} is 0-based, as in other cases[n] messages.
     */
    static CaseRequest canonical(FunctionDetails function, CaseRequest c, int position) {
        String label = "cases[" + position + "]";
        return new CaseRequest(
                canonicalInput(function, c.input(), label),
                canonicalOutput(function, c.expectedOutput(), label),
                c.isSample());
    }

    /** The same checks for cases already stored, such as a draft whose function changed after the upload. */
    static void check(FunctionDetails function, String input, String expectedOutput, int position) {
        String label = "cases[" + position + "]";
        canonicalInput(function, input, label);
        canonicalOutput(function, expectedOutput, label);
    }

    private static String canonicalInput(FunctionDetails function, String input, String label) {
        JsonNode root = parse(input, label + ".input");
        if (!root.isObject()) {
            throw new UnprocessableException(label + ".input must be a JSON object of the function's arguments");
        }
        List<FunctionParameter> parameters = function.parameters();
        ObjectNode ordered = STRICT.createObjectNode();
        for (FunctionParameter p : parameters) {
            JsonNode value = root.get(p.name());
            if (value == null) {
                throw new UnprocessableException(label + ".input is missing the argument '" + p.name() + "'");
            }
            if (!matches(value, p.type())) {
                throw new UnprocessableException(label + ".input argument '" + p.name() + "' must be " + p.type());
            }
            ordered.set(p.name(), value);
        }
        if (root.size() != parameters.size()) {
            throw new UnprocessableException(label + ".input has arguments the function does not take");
        }
        return STRICT.writeValueAsString(ordered);
    }

    private static String canonicalOutput(FunctionDetails function, String expectedOutput, String label) {
        JsonNode root = parse(expectedOutput, label + ".expected_output");
        if (!matches(root, function.returnType())) {
            throw new UnprocessableException(label + ".expected_output must be " + function.returnType());
        }
        return STRICT.writeValueAsString(root);
    }

    private static JsonNode parse(String text, String label) {
        try {
            return STRICT.readTree(text);
        } catch (JacksonException e) {
            // Not e.getMessage(): it quotes the text, which can be a hidden case.
            throw new UnprocessableException(label + " must be valid JSON");
        }
    }

    private static boolean matches(JsonNode value, String type) {
        if (type.endsWith("[]")) {
            if (!value.isArray()) {
                return false;
            }
            String element = type.substring(0, type.length() - 2);
            for (Iterator<JsonNode> it = value.iterator(); it.hasNext(); ) {
                if (!matches(it.next(), element)) {
                    return false;
                }
            }
            return true;
        }
        return switch (type) {
            case "int" -> value.isIntegralNumber() && value.canConvertToLong();
            case "float" -> value.isNumber() && Double.isFinite(value.asDouble());
            case "bool" -> value.isBoolean();
            case "string" -> value.isString();
            default -> false;
        };
    }
}
