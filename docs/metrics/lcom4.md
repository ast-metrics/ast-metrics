---
description: "LCOM4 measures class cohesion: whether the methods of a class belong together, and how to split a class that does too much."
---

# Class Cohesion (LCOM4)

LCOM4 answers a question you've asked in every code review: **is this class
one thing, or several things sharing a filename?** It doesn't judge style; it
counts, and the count is the number of classes hiding inside.

## Count it yourself, once

Draw an (imaginary) line between two methods when they touch the same field or
call each other. LCOM4 is the number of islands left when you're done:

```php
class UserService
{
    private Database $db;
    private Mailer $mailer;

    // Island 1: these two methods share $db...
    public function find(int $id): User
    {
        return $this->db->find($id);
    }

    public function save(User $user): void
    {
        $this->db->save($user);
    }

    // Island 2: ...and these two share $mailer. No bridge between islands.
    public function sendWelcome(string $email): void
    {
        $this->mailer->send($email, 'welcome');
    }

    public function sendGoodbye(string $email): void
    {
        $this->mailer->send($email, 'goodbye');
    }
}
```

`find` and `save` connect through `$db`. `sendWelcome` and `sendGoodbye`
connect through `$mailer`. Nothing connects the two groups: **LCOM4 = 2**.
This "service" is a repository and a notifier that happen to share a
constructor.

## How to read it

| LCOM4 | Meaning |
|-------|---------|
| **1** | Cohesive. All methods work on the same state. This is the goal. |
| **2+** | The class contains that many independent classes. |
| **0** | No methods (interfaces, empty classes). Nothing to measure. |

The beautiful property of LCOM4 is that it comes with its own refactoring
plan: the islands *are* the new classes.

```php
class UserRepository      // island 1, with $db
class WelcomeNotifier     // island 2, with $mailer
```

Split along the islands and nothing breaks, because by definition no method
of one island touched the state of the other.

## When to tolerate it

An LCOM4 of 2 is not automatically an emergency. Some patterns legitimately score
above 1: a class of pure static helpers, or a DTO whose getters each touch
their own field. Read the number together with size: a 40-line class at
LCOM4 = 2 is a shrug, a 400-line god class at LCOM4 = 4 is four refactorings
you already know how to do. On
[Monolog](../getting-started/your-first-analysis.md), the average is 1.47
with a maximum of 10: one class somewhere is really ten.

## See it on your code

```bash
ast-metrics analyze .
```

The `Coupling` block of the summary reports the average and maximum LCOM4.
The HTML report lists the score per class on the Classes page. To cap it, the
`architecture` ruleset ships a `max_responsibilities` rule for your
[`.ast-metrics.yaml`](../ci/linting-architecture.md):

```bash
ast-metrics ruleset add architecture
```
